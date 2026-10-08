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
import android.hardware.Sensor
import android.hardware.SensorEvent
import android.hardware.SensorEventListener
import android.hardware.SensorManager
import android.os.Looper
import android.os.SystemClock
import android.os.PowerManager
import java.io.BufferedReader
import java.io.InputStreamReader
import java.net.HttpURLConnection
import java.net.URL
import java.time.Instant
import java.time.LocalDateTime
import java.util.concurrent.atomic.AtomicBoolean

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

    /**
     * One stream thread and its open connection. A restart retires the old
     * link and starts a new one; the old thread sees [alive] go false and
     * ends on its own, so two threads never share a flag or a connection.
     */
    private class Link {
        @Volatile var alive = true
        @Volatile var conn: HttpURLConnection? = null
        var thread: Thread? = null

        fun stop() {
            alive = false
            conn?.disconnect() // unblocks a read
            thread?.interrupt() // cuts a backoff or poll sleep short
        }

        fun sleep(ms: Long) = try { Thread.sleep(ms); alive } catch (_: InterruptedException) { false }
    }
    private var link: Link? = null

    private var led: Logic.Led? = null
    private var wakeUntil: Instant = Instant.EPOCH
    private var wasScheduled: Boolean? = null

    // Wave-to-wake: the proximity sensor is only watched while the screen is
    // off by schedule, and only if the setting is on.
    private val sensors by lazy { getSystemService(SensorManager::class.java) }
    private val proximity by lazy { sensors.getDefaultSensor(Sensor.TYPE_PROXIMITY) }
    private val wave = Logic.Wave()
    private var watchingWave = false
    private val waveListener = object : SensorEventListener {
        override fun onSensorChanged(e: SensorEvent) {
            val near = e.values[0] < (proximity?.maximumRange ?: 5f)
            if (wave.onReading(near, SystemClock.elapsedRealtime())) {
                wakeUntil = Instant.now().plusSeconds(WAVE_PEEK_S)
                evaluate()
            }
        }

        override fun onAccuracyChanged(s: Sensor?, a: Int) {}
    }
    private var evaluating = false

    private val onChange: () -> Unit = {
        evaluate()
        Hub.state?.let { Ota.check(this, it) }
    }

    /** Reports to the server every minute, off the main thread. */
    private val beat = object : Runnable {
        override fun run() {
            Thread({ runCatching { Heartbeat.send(this@StreamService) } }, "scouter-heartbeat").start()
            main.postDelayed(this, HEARTBEAT_MS)
        }
    }
    private val tick = object : Runnable {
        override fun run() {
            evaluate()
            main.postDelayed(this, TICK_MS)
        }
    }

    override fun onCreate() {
        super.onCreate()
        prefs = Prefs(this)
        Crash.install(this)
        nm = getSystemService(NotificationManager::class.java)
        createChannels()
        startForeground(ID_SERVICE, Notification.Builder(this, CH_SERVICE).setSmallIcon(R.drawable.ic_scouter).setContentTitle("Scouter").setOngoing(true).build())
        wakeLock = getSystemService(PowerManager::class.java).newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "scouter:stream")
        wakeLock.acquire()
        Hub.listen(onChange)
        main.post(tick)
        main.postDelayed(beat, 10_000)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        restartStream() // config may have changed
        return START_STICKY
    }

    override fun onDestroy() {
        stopStream()
        Hub.unlisten(onChange)
        watchWave(false)
        main.removeCallbacks(tick)
        main.removeCallbacks(beat)
        if (wakeLock.isHeld) wakeLock.release()
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    // ---- stream -------------------------------------------------------------

    private fun restartStream() {
        stopStream()
        if (!prefs.configured) return
        val l = Link()
        link = l
        l.thread = Thread({ streamLoop(l) }, "scouter-stream").also { it.start() }
    }

    /** Retires the current link without waiting for its thread: it exits by itself. */
    private fun stopStream() {
        link?.stop()
        link = null
    }

    /**
     * Prefers the live stream. If a stream connects but delivers no state
     * within [FIRST_STATE_MS] (a buffering proxy holds it back), it falls back
     * to polling /v1/state for [POLL_FOR_MS], then tries the stream again.
     */
    private fun streamLoop(link: Link) {
        var backoff = Logic.BACKOFF_MIN_MS
        var pollUntil = 0L
        var etag: String? = null
        while (link.alive) {
            val now = System.currentTimeMillis()
            if (now < pollUntil) {
                Hub.postPolling(true)
                etag = runCatching { pollOnce(link, etag) }.getOrElse { Hub.postConnected(false); etag }
                if (!link.sleep(POLL_EVERY_MS)) break
                continue
            }
            Hub.postPolling(false)
            val outcome = runCatching { readStream(link) }.getOrDefault(StreamOutcome.FAILED)
            if (!link.alive) break // retired while connected: don't touch shared state
            when (Logic.afterStream(outcome)) {
                Logic.Next.POLL -> pollUntil = System.currentTimeMillis() + POLL_FOR_MS
                Logic.Next.RECONNECT_NOW -> {}
                Logic.Next.BACK_OFF -> {
                    Hub.postConnected(false)
                    if (!link.sleep(backoff)) break
                }
            }
            backoff = Logic.nextBackoff(backoff, outcome)
        }
    }

    /** One GET /v1/state; returns the ETag to send next time. */
    private fun pollOnce(link: Link, etag: String?): String? {
        val c = URL(prefs.url + "/v1/state").openConnection() as HttpURLConnection
        link.conn = c
        if (!link.alive) return etag // stopped before it could see this connection
        try {
            c.connectTimeout = 10_000
            c.readTimeout = 20_000
            c.setRequestProperty("Authorization", "Bearer " + prefs.token)
            etag?.let { c.setRequestProperty("If-None-Match", it) }
            return when (c.responseCode) {
                200 -> {
                    Hub.postState(Parser.parse(c.inputStream.bufferedReader().readText()))
                    c.getHeaderField("ETag")
                }
                304 -> { Hub.postConnected(true); etag }
                else -> throw IllegalStateException("HTTP ${c.responseCode}")
            }
        } finally {
            c.disconnect()
            link.conn = null
        }
    }

    /**
     * Minimal SSE reader: the aggregator only sends `event: state` with one
     * data line, and pings. A watchdog cuts the connection when no state
     * arrives in time; "connected" means data arrived, not just a 200.
     */
    private fun readStream(link: Link): StreamOutcome {
        val c = URL(prefs.url + "/v1/stream").openConnection() as HttpURLConnection
        link.conn = c
        // Both fields are volatile: either stop() sees this connection, or we see it stopped.
        if (!link.alive) return StreamOutcome.FAILED
        c.connectTimeout = 10_000
        c.readTimeout = 70_000 // heartbeat is 25 s; two missed means the link is dead
        c.setRequestProperty("Authorization", "Bearer " + prefs.token)
        c.setRequestProperty("Accept", "text/event-stream")
        // Shared with the watchdog, which runs on the main thread.
        val gotState = AtomicBoolean(false)
        val starved = AtomicBoolean(false)
        val watchdog = Runnable { if (!gotState.get()) { starved.set(true); c.disconnect() } }
        try {
            if (c.responseCode != 200) throw IllegalStateException("HTTP ${c.responseCode}")
            main.postDelayed(watchdog, FIRST_STATE_MS)
            val sse = Logic.Sse()
            BufferedReader(InputStreamReader(c.inputStream, Charsets.UTF_8)).use { r ->
                while (link.alive) {
                    val line = r.readLine() ?: break
                    val event = sse.feed(line) ?: continue
                    // Parsed first: a document that cannot be read is a
                    // failure (back off), not a working stream (reconnect now).
                    Hub.postState(Parser.parse(event))
                    gotState.set(true)
                }
            }
        } catch (_: Exception) {
            // A drop after data is routine (network blip, deploy): the
            // outcome below says how far the attempt got.
        } finally {
            main.removeCallbacks(watchdog)
            c.disconnect()
            link.conn = null
        }
        return Logic.streamOutcome(gotState.get(), starved.get())
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
        // Kept in prefs: after a crash or an update, alerts that already woke
        // the screen must not wake it again.
        val alerted = prefs.alerted
        val fresh = s?.let { Logic.openAlerts(it, dismissed) }.orEmpty().filter { it.id !in alerted }
        if (fresh.isNotEmpty()) wakeUntil = now.plusSeconds(ALERT_WAKE_S)
        if (s != null) {
            val keep = (alerted + fresh.map { it.id }).intersect(s.alerts.map { it.id }.toSet())
            if (keep != alerted) prefs.alerted = keep
        }
        // Everything dismissed: an alert-woken screen may go dark again now.
        if (s != null && Logic.openAlerts(s, dismissed).isEmpty()) wakeUntil = Instant.EPOCH

        val scheduled = schedule().isOn(LocalDateTime.now())
        val want = scheduled || now.isBefore(wakeUntil)
        val was = Hub.wantScreenOn

        // Morning: the schedule (not an alert or a wave) turned the screen on.
        // Only the server's schedule counts: right after start the local
        // fallback can say "off" and the first document "on", which is not a morning.
        if (s != null && s.hasSettings) {
            if (scheduled && wasScheduled == false && s.settings.briefing) {
                Hub.briefingSince = prefs.screenOffAt
                Hub.briefingUntil = now.plusSeconds(BRIEFING_S)
            }
            if (!scheduled && wasScheduled == true) prefs.screenOffAt = now
            wasScheduled = scheduled
        }
        watchWave(!scheduled && s?.settings?.wave != false)
        Hub.wantScreenOn = want
        if (want && (!was || fresh.isNotEmpty())) wakeScreen()
        if (!want && was) sleepScreen()

        setLed(Logic.led(s, Hub.connected, dismissed))

        // Self-healing kiosk: whatever closed the dashboard, bring it back so
        // it can lock the phone again (it re-applies kiosk on resume).
        if (s?.settings?.kiosk == true && want && Kiosk.isOwner(this) && !Kiosk.isLocked(this)) {
            startActivity(Intent(this, DashboardActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_REORDER_TO_FRONT))
        }
    }

    /**
     * The server's schedule once a document has arrived. The adb-set local one
     * covers only the time before that, or a server too old to send settings.
     */
    private fun schedule(): Logic.Schedule {
        val s = Hub.state
        return if (s != null && s.hasSettings) Logic.Schedule.of(s.settings) else Logic.Schedule.parse(prefs.schedule)
    }

    private fun watchWave(on: Boolean) {
        val sensor = proximity ?: return
        if (on && !watchingWave) watchingWave = sensors.registerListener(waveListener, sensor, SensorManager.SENSOR_DELAY_NORMAL)
        if (!on && watchingWave) {
            sensors.unregisterListener(waveListener)
            watchingWave = false
        }
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
        private const val FIRST_STATE_MS = 30_000L
        private const val HEARTBEAT_MS = 60_000L
        private const val WAVE_PEEK_S = 20L
        private const val BRIEFING_S = 30L
        private const val POLL_EVERY_MS = 30_000L
        private const val POLL_FOR_MS = 10 * 60_000L
        private const val ALERT_WAKE_S = 5 * 60L
        private const val ID_SERVICE = 1
        private const val ID_LED = 2
        private const val CH_SERVICE = "service"
        private val LED_COLORS = mapOf(
            Logic.Led.GREEN to 0xFF00FF00.toInt(),
            Logic.Led.AMBER to 0xFFFF8000.toInt(),
            Logic.Led.RED to 0xFFFF0000.toInt(),
            Logic.Led.BLUE to 0xFF0040FF.toInt(),
            Logic.Led.PURPLE to 0xFF8000FF.toInt(),
        )

        fun start(ctx: Context) = ctx.startForegroundService(Intent(ctx, StreamService::class.java))
    }
}
