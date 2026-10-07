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
import kotlin.math.abs
import kotlin.random.Random

/**
 * Full-screen kiosk. Configure over adb:
 *   adb shell am start -n dev.recica.scouter/.DashboardActivity --es url https://… --es token … [--es schedule "1-5 09:00-19:00"]
 * Swipe left/right: Focus ⇄ Grid. Long-press in Focus: pin/unpin. Tap a tile: focus it. Tap an alert: dismiss.
 */
class DashboardActivity : Activity() {
    private lateinit var prefs: Prefs
    private lateinit var view: DashboardView
    private val main = Handler(Looper.getMainLooper())
    private val redraw: () -> Unit = { applyScreenFlag(); view.invalidate() }

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
        configure(intent)
        setShowWhenLocked(true)
        setTurnScreenOn(true)
        view = DashboardView(this)
        view.pinned = prefs.pinned
        view.dismissed = prefs.dismissed
        setContentView(view)
        val gestures = GestureDetector(this, Gestures())
        view.setOnTouchListener { _, e -> gestures.onTouchEvent(e); true }
        StreamService.start(this)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setTurnScreenOn(true)
        if (configure(intent)) StreamService.start(this)
        view.invalidate()
    }

    /** Applies adb-provided settings; returns true if anything changed. */
    private fun configure(i: Intent?): Boolean {
        var changed = false
        i?.getStringExtra("url")?.let { prefs.url = it; changed = true }
        i?.getStringExtra("token")?.let { prefs.token = it; changed = true }
        i?.getStringExtra("schedule")?.let { prefs.schedule = it; changed = true }
        return changed
    }

    override fun onResume() {
        super.onResume()
        immersive()
        Hub.listen(redraw)
        applyScreenFlag()
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
            val onBanner = e.y < view.bannerHeight()
            val cardShowing = Logic.openAlerts(s, prefs.dismissed).any { a ->
                Hub.alertSeenAt[a.id]?.let { java.time.Duration.between(it, java.time.Instant.now()).seconds < DashboardView.CARD_SECONDS } == true
            }
            if ((cardShowing || onBanner) && dismissAlerts()) return true
            if (view.mode == DashboardView.Mode.GRID) {
                view.tiles.firstOrNull { it.first.contains(e.x - view.translationX, e.y - view.translationY) }?.let { (_, p) ->
                    prefs.pinned = p.fullName
                    view.pinned = p.fullName
                    view.mode = DashboardView.Mode.FOCUS
                    view.invalidate()
                }
            }
            return true
        }

        override fun onLongPress(e: MotionEvent) {
            if (view.mode != DashboardView.Mode.FOCUS) return
            val s = Hub.state ?: return
            val current = Logic.focus(s, prefs.pinned) ?: return
            prefs.pinned = if (prefs.pinned == current.fullName) null else current.fullName
            view.pinned = prefs.pinned
            view.performHapticFeedback(View.HAPTIC_FEEDBACK_ENABLED)
            view.invalidate()
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

/** Brings the dashboard back after a reboot. */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return
        StreamService.start(ctx)
        ctx.startActivity(Intent(ctx, DashboardActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }
}
