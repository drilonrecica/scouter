package dev.recica.scouter

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import java.util.concurrent.Executors

/**
 * Over-the-air updates: when the server offers an APK whose hash this phone
 * has not tried yet, download it, check the hash and install it with a
 * package-installer session. As device owner that is silent. Android itself
 * refuses other signing keys and downgrades. Each hash is installed at most
 * once, so a broken upload cannot cause an install loop; a download that
 * fails or arrives damaged is retried, at most every [RETRY_MS].
 */
object Ota {
    private const val RETRY_MS = 5 * 60_000L
    private val io = Executors.newSingleThreadExecutor()
    @Volatile private var busy = false
    @Volatile private var lastAttempt = 0L

    fun check(ctx: Context, s: DashState) {
        val app = s.app ?: return
        val prefs = Prefs(ctx.applicationContext)
        if (busy || app.sha256 == prefs.otaTried || !Kiosk.isOwner(ctx)) return
        val now = System.currentTimeMillis()
        if (now - lastAttempt < RETRY_MS) return
        lastAttempt = now
        busy = true
        val appCtx = ctx.applicationContext
        io.execute {
            try {
                install(appCtx, prefs, app)
            } catch (e: Exception) {
                prefs.otaResult = "failed: ${e.message ?: e.javaClass.simpleName}"
            } finally {
                busy = false
            }
        }
    }

    private fun install(ctx: Context, prefs: Prefs, app: AppRelease) {
        val file = File(ctx.cacheDir, "update.apk")
        val c = URL(prefs.url + "/v1/app.apk").openConnection() as HttpURLConnection
        try {
            c.connectTimeout = 15_000
            c.readTimeout = 60_000
            c.setRequestProperty("Authorization", "Bearer " + prefs.token)
            check(c.responseCode == 200) { "download: HTTP ${c.responseCode}" }
            c.inputStream.use { input -> file.outputStream().use { input.copyTo(it) } }
        } finally {
            c.disconnect()
        }
        val sha = MessageDigest.getInstance("SHA-256").digest(file.readBytes()).joinToString("") { "%02x".format(it) }
        check(sha == app.sha256) { "hash mismatch" }
        // From here on, one attempt per upload: a failed install is not retried.
        // Saved synchronously, since a successful install replaces this process.
        prefs.markOtaTried(app.sha256)

        val installer = ctx.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL).apply {
            setAppPackageName(ctx.packageName)
        }
        val id = installer.createSession(params)
        installer.openSession(id).use { session ->
            session.openWrite("scouter.apk", 0, file.length()).use { out ->
                file.inputStream().use { it.copyTo(out) }
                session.fsync(out)
            }
            val done = PendingIntent.getBroadcast(
                ctx, id, Intent(ctx, OtaReceiver::class.java),
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE,
            )
            prefs.otaResult = "installing ${app.sha256.take(8)}"
            session.commit(done.intentSender)
        }
        file.delete()
    }
}

/** Receives the installer's verdict. On success the process is replaced; [BootReceiver] restarts it. */
class OtaReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)
        val msg = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE).orEmpty()
        Prefs(ctx).otaResult = if (status == PackageInstaller.STATUS_SUCCESS) "installed" else "failed ($status): $msg"
    }
}
