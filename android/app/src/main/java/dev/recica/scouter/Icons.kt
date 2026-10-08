package dev.recica.scouter

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.Executors

/**
 * Project icons by hash: in memory, then in filesDir/icons, then from the
 * aggregator. A hash names its content, so a stored icon never goes stale;
 * icons no project uses any more are deleted. Main thread, except the I/O.
 */
object Icons {
    private val io = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    private val bitmaps = HashMap<String, Bitmap>()
    private val loading = HashSet<String>()
    private val failedAt = HashMap<String, Long>()
    private const val RETRY_MS = 10 * 60_000L

    /** The icon, or null while it loads or when there is none. */
    fun get(hash: String?): Bitmap? = hash?.let { bitmaps[it] }

    /** Loads every icon the document names and forgets the others; redraws once each arrives. */
    fun sync(ctx: Context, s: DashState) {
        val app = ctx.applicationContext
        val want = s.projects.mapNotNull { it.icon }.toSet()
        bitmaps.keys.retainAll(want)
        val now = SystemClock.elapsedRealtime()
        for (h in want) {
            if (h in bitmaps || h in loading) continue
            failedAt[h]?.let { if (now - it < RETRY_MS) continue }
            loading += h
            io.execute {
                val bmp = runCatching { load(app, h) }.getOrNull()
                main.post {
                    loading -= h
                    if (bmp != null) {
                        bitmaps[h] = bmp
                        failedAt -= h
                        Hub.notifyListeners()
                    } else {
                        failedAt[h] = SystemClock.elapsedRealtime()
                    }
                }
            }
        }
        io.execute { dir(app).listFiles()?.forEach { if (it.nameWithoutExtension !in want) it.delete() } }
    }

    private fun dir(ctx: Context) = File(ctx.filesDir, "icons").apply { mkdirs() }

    private fun load(ctx: Context, hash: String): Bitmap? {
        val file = File(dir(ctx), "$hash.png")
        if (!file.exists()) {
            val prefs = Prefs(ctx)
            val c = URL(prefs.url + "/v1/icons/" + hash).openConnection() as HttpURLConnection
            try {
                c.connectTimeout = 10_000
                c.readTimeout = 10_000
                c.setRequestProperty("Authorization", "Bearer " + prefs.token)
                if (c.responseCode != 200) return null
                val tmp = File(file.path + ".tmp")
                c.inputStream.use { i -> tmp.outputStream().use { i.copyTo(it) } }
                tmp.renameTo(file)
            } finally {
                c.disconnect()
            }
        }
        return BitmapFactory.decodeFile(file.path)
    }
}
