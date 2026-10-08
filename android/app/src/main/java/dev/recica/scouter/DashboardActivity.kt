package dev.recica.scouter

import android.app.Activity
import android.app.admin.DeviceAdminReceiver
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.GestureDetector
import android.view.MotionEvent
import android.view.View
import android.view.WindowInsets
import android.view.WindowInsetsController
import android.view.WindowManager
import android.widget.Toast
import kotlin.math.abs
import kotlin.random.Random

/**
 * Full-screen kiosk; configuration arrives through [ConfigReceiver], never
 * through this exported activity's intent.
 * Swipe left/right: Focus ⇄ Grid. Long-press the top strip: settings. Long-press elsewhere
 * in Focus: pin/unpin. Tap a tile: pin it. Tap an alert: dismiss.
 */
class DashboardActivity : Activity() {
    private lateinit var prefs: Prefs
    private lateinit var view: DashboardView
    private val main = Handler(Looper.getMainLooper())
    private var shown: DashState? = null

    /** Hub changes: a new document starts the scan animation, anything else just redraws. */
    /** Kiosk state last applied; null forces a re-apply (on resume). */
    private var appliedKiosk: Boolean? = null

    private val redraw: () -> Unit = {
        applyScreenFlag()
        Hub.state?.settings?.kiosk?.let { k ->
            if (k != appliedKiosk) {
                Kiosk.apply(this, k)
                appliedKiosk = k
            }
        }
        val s = Hub.state
        if (s != null && s.version != shown?.version) {
            view.onStateChanged(shown, s)
            shown = s
        } else {
            view.invalidate()
        }
    }

    /** Burn-in guard: nudge everything a few pixels each minute. */
    private val shift = object : Runnable {
        override fun run() {
            val max = 4 * resources.displayMetrics.density
            view.translationX = Random.nextFloat() * 2 * max - max
            view.translationY = Random.nextFloat() * 2 * max - max
            view.invalidate() // also refreshes "3m ago" texts
            main.postDelayed(this, 60_000)
        }
    }

    /** A running build's timer ticks every second, only while visible. */
    private val seconds = object : Runnable {
        override fun run() {
            if (Hub.state?.projects?.any { it.ci?.status == Status.RUNNING || it.latest?.status == Status.RUNNING } == true) view.invalidate()
            main.postDelayed(this, 1_000)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        prefs = Prefs(this)
        setShowWhenLocked(true)
        setTurnScreenOn(true)
        view = DashboardView(this)
        view.dismissed = prefs.dismissed
        // Rendered into a GPU texture once and only re-rendered when it changes:
        // the scan line animates on top without re-rasterising the HUD.
        view.setLayerType(View.LAYER_TYPE_HARDWARE, null)
        val sweep = SweepView(this)
        view.sweep = sweep
        setContentView(android.widget.FrameLayout(this).apply {
            addView(view)
            addView(sweep) // on top, transparent, ignores touches
        })
        val gestures = GestureDetector(this, Gestures())
        view.setOnTouchListener { _, e -> gestures.onTouchEvent(e); true }
        StreamService.start(this)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setTurnScreenOn(true)
        appliedKiosk = null // brought back by the service: re-apply kiosk
        redraw()
        view.invalidate()
    }

    /**
     * The dashboard is the root screen: Back has nowhere to go. In kiosk mode
     * finishing it would also end lock task, so Back is ignored outright.
     */
    @Deprecated("Deprecated in Java")
    override fun onBackPressed() {}

    override fun onResume() {
        super.onResume()
        immersive()
        Hub.listen(redraw)
        appliedKiosk = null
        redraw() // catch up on anything that arrived while paused
        main.post(shift)
        main.post(seconds)
    }

    override fun onPause() {
        Hub.unlisten(redraw)
        main.removeCallbacks(shift)
        main.removeCallbacks(seconds)
        super.onPause()
    }

    private fun applyScreenFlag() {
        if (Hub.wantScreenOn) window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        else window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
    }

    private fun immersive() {
        window.insetsController?.let {
            it.hide(WindowInsets.Type.systemBars())
            it.systemBarsBehavior = WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }
    }

    private fun save(next: Settings) = Api.saveSettings(this, next) { err ->
        if (err != null) Toast.makeText(this, "Not saved: $err", Toast.LENGTH_LONG).show()
    }

    private fun dismissAlerts(): Boolean {
        val s = Hub.state ?: return false
        val open = Logic.openAlerts(s, prefs.dismissed)
        if (open.isEmpty()) return false
        // Keep only IDs still in the document so the set cannot grow forever.
        val live = s.alerts.map { it.id }.toSet()
        prefs.dismissed = (prefs.dismissed intersect live) + open.map { it.id }
        view.dismissed = prefs.dismissed
        Hub.notifyListeners() // LED follows
        return true
    }

    private inner class Gestures : GestureDetector.SimpleOnGestureListener() {
        override fun onDown(e: MotionEvent) = true

        override fun onSingleTapUp(e: MotionEvent): Boolean {
            val s = Hub.state ?: return false
            if (view.briefingShown(java.time.Instant.now())) {
                Hub.briefingUntil = java.time.Instant.EPOCH
                view.invalidate()
                return true
            }
            val onBanner = e.y < view.bannerHeight()
            val cardShowing = Logic.openAlerts(s, prefs.dismissed).any { a ->
                Hub.alertSeenAt[a.id]?.let { java.time.Duration.between(it, java.time.Instant.now()).seconds < DashboardView.CARD_SECONDS } == true
            }
            if ((cardShowing || onBanner) && dismissAlerts()) return true
            if (view.mode == DashboardView.Mode.GRID) {
                view.tiles.firstOrNull { it.first.contains(e.x - view.translationX, e.y - view.translationY) }?.let { (_, p) ->
                    save(s.settings.copy(focusMode = "pinned", pinned = p.fullName))
                    view.mode = DashboardView.Mode.FOCUS
                    view.invalidate()
                }
            }
            return true
        }

        override fun onLongPress(e: MotionEvent) {
            view.performHapticFeedback(View.HAPTIC_FEEDBACK_ENABLED)
            if (e.y < view.headerHeight()) {
                startActivity(Intent(this@DashboardActivity, SettingsActivity::class.java))
                return
            }
            if (view.mode != DashboardView.Mode.FOCUS) return
            val s = Hub.state ?: return
            val current = Logic.focus(s) ?: return
            save(Logic.togglePin(s.settings, current.fullName))
        }

        override fun onFling(e1: MotionEvent?, e2: MotionEvent, vx: Float, vy: Float): Boolean {
            if (abs(vx) < abs(vy) || abs(vx) < 500) return false
            val modes = DashboardView.Mode.entries
            val step = if (vx < 0) 1 else -1
            view.mode = modes[(view.mode.ordinal + step + modes.size) % modes.size]
            view.invalidate()
            return true
        }
    }
}

/** Device admin, used only for lockNow() to switch the screen off outside the schedule. */
class AdminReceiver : DeviceAdminReceiver()

/**
 * Configuration over adb only:
 *   adb shell am broadcast -n dev.recica.scouter/.ConfigReceiver --es url https://… --es token … [--es schedule "1-5 09:00-19:00"]
 *   adb shell am broadcast -n dev.recica.scouter/.ConfigReceiver --ez release_owner true
 * The manifest guards it with android.permission.DUMP, which the adb shell
 * holds and ordinary apps cannot get; otherwise any app could point the
 * phone at its own server and collect the bearer token.
 */
class ConfigReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        val prefs = Prefs(ctx)
        val url = intent.getStringExtra("url")
        val token = intent.getStringExtra("token")
        // Recovery path when the UI is unreachable: give up device ownership.
        if (intent.getBooleanExtra("release_owner", false)) {
            Kiosk.release(ctx)
            resultData = "released"
            return
        }
        // A new server only together with its own token: never send the
        // current token to a URL that arrived without one.
        if (url != null && token == null) {
            resultData = "rejected: url needs a token in the same broadcast"
            return
        }
        url?.let { prefs.url = it }
        token?.let { prefs.token = it }
        intent.getStringExtra("schedule")?.let { prefs.schedule = it }
        resultData = "ok"
        StreamService.start(ctx)
    }
}

/** Brings the dashboard back after a reboot or an update. */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        // Also after our own update: the installer replaces the process and
        // nothing else would bring the dashboard (and kiosk) back.
        if (intent.action != Intent.ACTION_BOOT_COMPLETED && intent.action != Intent.ACTION_MY_PACKAGE_REPLACED) return
        StreamService.start(ctx)
        ctx.startActivity(Intent(ctx, DashboardActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }
}
