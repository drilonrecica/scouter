package dev.recica.scouter

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Canvas
import android.view.View
import android.view.animation.DecelerateInterpolator

/**
 * The scan line, drawn on a transparent overlay above the dashboard. With
 * hardware acceleration, animating this view leaves the dashboard's recorded
 * drawing untouched: each frame costs one line instead of the whole HUD.
 */
class SweepView(ctx: Context) : View(ctx) {
    private val hud = Hud(resources.displayMetrics.density)
    private var t = 1f
    private val animator = ValueAnimator.ofFloat(0f, 1f).apply {
        duration = 650
        interpolator = DecelerateInterpolator()
        addUpdateListener {
            t = it.animatedValue as Float
            invalidate()
        }
    }

    init {
        isClickable = false
        isFocusable = false
    }

    fun scan() {
        animator.cancel()
        animator.start()
    }

    override fun onDraw(c: Canvas) {
        if (t >= 1f) return
        val d = resources.displayMetrics.density
        hud.sweep(c, width.toFloat(), 10 * d + (height - 20 * d) * t)
    }

    override fun onDetachedFromWindow() {
        animator.cancel()
        super.onDetachedFromWindow()
    }
}
