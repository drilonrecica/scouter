package dev.recica.scouter

import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.wifi.WifiManager
import android.os.BatteryManager
import android.os.Debug
import android.os.SystemClock
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.time.Instant

/** The phone's minute-by-minute report, so the server notices a dead or overheating phone. */
object Heartbeat {
    fun send(ctx: Context) {
        val prefs = Prefs(ctx)
        if (!prefs.configured) return
        val battery = ctx.registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
        val level = battery?.let { it.getIntExtra(BatteryManager.EXTRA_LEVEL, -1) * 100 / it.getIntExtra(BatteryManager.EXTRA_SCALE, 100).coerceAtLeast(1) } ?: -1
        val status = battery?.getIntExtra(BatteryManager.EXTRA_STATUS, -1)
        val pkg = ctx.packageManager.getPackageInfo(ctx.packageName, 0)
        @Suppress("DEPRECATION")
        val rssi = runCatching { ctx.getSystemService(WifiManager::class.java).connectionInfo.rssi }.getOrDefault(0)
        val body = JSONObject()
            .put("app_version", pkg.versionName)
            .put("version_code", pkg.longVersionCode)
            .put("battery", level)
            .put("charging", status == BatteryManager.BATTERY_STATUS_CHARGING || status == BatteryManager.BATTERY_STATUS_FULL)
            .put("temp_c", (battery?.getIntExtra(BatteryManager.EXTRA_TEMPERATURE, 0) ?: 0) / 10.0)
            .put("uptime_s", SystemClock.elapsedRealtime() / 1000)
            .put("pss_kb", Debug.getPss())
            .put("wifi_rssi", rssi)
            .put("device_owner", Kiosk.isOwner(ctx))
            .put("lock_task", Kiosk.isLocked(ctx))
            .put("last_ota", prefs.otaResult)
            .put("last_crash", prefs.lastCrash)
        val c = URL(prefs.url + "/v1/heartbeat").openConnection() as HttpURLConnection
        try {
            c.requestMethod = "POST"
            c.connectTimeout = 10_000
            c.readTimeout = 10_000
            c.doOutput = true
            c.setRequestProperty("Authorization", "Bearer " + prefs.token)
            c.setRequestProperty("Content-Type", "application/json")
            c.outputStream.use { it.write(body.toString().toByteArray()) }
            c.responseCode // 404 from an older server is fine: nothing to do
        } finally {
            c.disconnect()
        }
    }
}

/**
 * Records an uncaught exception for the heartbeat, then hands it to Android
 * as before, which ends the process; START_STICKY brings the service back.
 */
object Crash {
    @Volatile private var installed = false

    fun install(ctx: Context) {
        if (installed) return
        installed = true
        val app = ctx.applicationContext
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { t, e ->
            runCatching { Prefs(app).lastCrash = "${Instant.now()} [${t.name}] ${e.stackTraceToString().take(2000)}" }
            previous?.uncaughtException(t, e)
        }
    }
}
