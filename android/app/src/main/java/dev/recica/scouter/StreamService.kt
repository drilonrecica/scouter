package dev.recica.scouter

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.app.admin.DevicePolicyManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.PowerManager
import java.io.BufferedReader
import java.io.InputStreamReader
import java.net.HttpURLConnection
import java.net.URL
import java.time.Instant
import java.time.LocalDateTime

/**
 * Holds the SSE connection to the aggregator, drives the RGB LED and owns
 * the screen schedule. The phone lives on USB power, so a partial wake lock
 * is cheap and keeps the stream and the schedule tick alive with the screen off.
 */
class StreamService : Service() {
    private lateinit var prefs: Prefs
    private lateinit var nm: NotificationManager
    private lateinit var wakeLock: PowerManager.WakeLock
    private val main = Handler(Looper.getMainLooper())

    @Volatile private var running = false
    @Volatile private var conn: HttpURLConnection? = null
    private var thread: Thread? = null

    private var led: Logic.Led? = null
    private var wakeUntil: Instant = Instant.EPOCH
    private var alerted = mutableSetOf<String>()
    private var evaluating = false

    private val onChange: () -> Unit = { evaluate() }
    private val tick = object : Runnable {
        override fun run() {
            evaluate()
            main.postDelayed(this, TICK_MS)
        }
    }

    override fun onCreate() {
        super.onCreate()
        prefs = Prefs(this)
        nm = getSystemService(NotificationManager::class.java)
        createChannels()
        startForeground(ID_SERVICE, Notification.Builder(this, CH_SERVICE).setSmallIcon(R.drawable.ic_scouter).setContentTitle("Scouter").setOngoing(true).build())
        wakeLock = getSystemService(PowerManager::class.java).newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "scouter:stream")
        wakeLock.acquire()
        Hub.listen(onChange)
        main.post(tick)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        restartStream() // config may have changed
        return START_STICKY
    }

    override fun onDestroy() {
        stopStream()
        Hub.unlisten(onChange)
        main.removeCallbacks(tick)
        if (wakeLock.isHeld) wakeLock.release()
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    // ---- stream -------------------------------------------------------------

    private fun restartStream() {
        stopStream()
        if (!prefs.configured) return
        running = true
        thread = Thread(::streamLoop, "scouter-stream").also { it.start() }
    }

    private fun stopStream() {
        running = false
        conn?.disconnect() // unblocks the reader
        thread?.join(2000)
        thread = null
    }

    private fun streamLoop() {
        var backoff = 2_000L
        while (running) {
            try {
                readStream()
                backoff = 2_000L
            } catch (_: Exception) {
            }
            Hub.postConnected(false)
            if (!running) break
            try { Thread.sleep(backoff) } catch (_: InterruptedException) { break }
            backoff = (backoff * 2).coerceAtMost(60_000L)
        }
    }

    /** Minimal SSE reader: the aggregator only sends `event: state` with one data line, and pings. */
    private fun readStream() {
        val c = URL(prefs.url + "/v1/stream").openConnection() as HttpURLConnection
        conn = c
        c.connectTimeout = 10_000
        c.readTimeout = 70_000 // heartbeat is 25 s; two missed means the link is dead
        c.setRequestProperty("Authorization", "Bearer " + prefs.token)
        c.setRequestProperty("Accept", "text/event-stream")
        try {
            if (c.responseCode != 200) throw IllegalStateException("HTTP ${c.responseCode}")
            Hub.postConnected(true)
            val data = StringBuilder()
            BufferedReader(InputStreamReader(c.inputStream, Charsets.UTF_8)).use { r ->
                while (running) {
                    val line = r.readLine() ?: break
                    when {
                        line.startsWith("data:") -> data.append(line.substring(5).trimStart())
                        line.isEmpty() && data.isNotEmpty() -> {
                            Hub.postState(Parser.parse(data.toString()))
                            data.clear()
                        }
                    }
                }
            }
        } finally {
            c.disconnect()
            conn = null
        }
    }

    // ---- screen + LED --------------------------------------------------------

    /** Runs on every state change and every tick; all decisions are in [Logic]. */
    private fun evaluate() {
        // Setting Hub.wantScreenOn notifies listeners, us included: don't recurse.
        if (evaluating) return
        evaluating = true
        try { evaluateOnce() } finally { evaluating = false }
    }

    private fun evaluateOnce() {
        val now = Instant.now()
        val s = Hub.state
        val dismissed = prefs.dismissed
        val fresh = s?.let { Logic.openAlerts(it, dismissed) }.orEmpty().filter { it.id !in alerted }
        if (fresh.isNotEmpty()) {
            alerted.addAll(fresh.map { it.id })
            wakeUntil = now.plusSeconds(ALERT_WAKE_S)
        }
        s?.let { st -> alerted.retainAll(st.alerts.map { it.id }.toSet()) }
        // Everything dismissed: an alert-woken screen may go dark again now.
        if (s != null && Logic.openAlerts(s, dismissed).isEmpty()) wakeUntil = Instant.EPOCH

        val want = Logic.Schedule.parse(prefs.schedule).isOn(LocalDateTime.now()) || now.isBefore(wakeUntil)
        val was = Hub.wantScreenOn
        Hub.wantScreenOn = want
        if (want && (!was || fresh.isNotEmpty())) wakeScreen()
        if (!want && was) sleepScreen()

        setLed(Logic.led(s, Hub.connected, dismissed))
    }

    private fun wakeScreen() {
        // Brief full wake lock turns the panel on; the activity's
        // FLAG_KEEP_SCREEN_ON keeps it on from there.
        @Suppress("DEPRECATION")
        getSystemService(PowerManager::class.java)
            .newWakeLock(PowerManager.SCREEN_BRIGHT_WAKE_LOCK or PowerManager.ACQUIRE_CAUSES_WAKEUP, "scouter:wake")
            .acquire(5_000)
        startActivity(Intent(this, DashboardActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_REORDER_TO_FRONT))
    }

    private fun sleepScreen() {
        val dpm = getSystemService(DevicePolicyManager::class.java)
        if (dpm.isAdminActive(ComponentName(this, AdminReceiver::class.java))) dpm.lockNow()
    }

    /**
     * The LED shows only while the screen is off, and only for a fresh
     * notification, so a colour change cancels and re-posts on its own channel
     * (a channel's light colour is fixed once created).
     */
    private fun setLed(l: Logic.Led) {
        if (l == led) return
        led = l
        nm.cancel(ID_LED)
        nm.notify(
            ID_LED,
            Notification.Builder(this, "led_" + l.name.lowercase())
                .setSmallIcon(R.drawable.ic_scouter)
                .setContentTitle("Scouter: " + l.name.lowercase())
                .setOnlyAlertOnce(false)
                .build(),
        )
    }

    private fun createChannels() {
        nm.createNotificationChannel(NotificationChannel(CH_SERVICE, "Connection", NotificationManager.IMPORTANCE_MIN))
        for ((led, argb) in LED_COLORS) {
            nm.createNotificationChannel(
                NotificationChannel("led_" + led.name.lowercase(), "Status light: " + led.name.lowercase(), NotificationManager.IMPORTANCE_DEFAULT).apply {
                    enableLights(true)
                    lightColor = argb
                    setSound(null, null)
                    enableVibration(false)
                    setShowBadge(false)
                },
            )
        }
    }

    companion object {
        private const val TICK_MS = 30_000L
        private const val ALERT_WAKE_S = 5 * 60L
        private const val ID_SERVICE = 1
        private const val ID_LED = 2
        private const val CH_SERVICE = "service"
        private val LED_COLORS = mapOf(
            Logic.Led.GREEN to 0xFF00FF00.toInt(),
            Logic.Led.AMBER to 0xFFFF8000.toInt(),
            Logic.Led.RED to 0xFFFF0000.toInt(),
            Logic.Led.PURPLE to 0xFF8000FF.toInt(),
        )

        fun start(ctx: Context) = ctx.startForegroundService(Intent(ctx, StreamService::class.java))
    }
}
