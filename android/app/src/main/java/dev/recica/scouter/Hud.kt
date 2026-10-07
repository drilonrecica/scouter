package dev.recica.scouter

import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RectF
import kotlin.math.cos
import kotlin.math.sin
import kotlin.random.Random

/**
 * Scouter HUD chrome, drawn in code: thin lines on black only, so it costs
 * next to nothing on AMOLED and nothing is borrowed from the anime's artwork.
 */
class Hud(private val density: Float) {
    private fun dp(v: Float) = v * density

    val chrome = stroke(CHROME, 1.5f)
    val chromeDim = stroke(CHROME_DIM, 1f)
    private val fill = Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.FILL }

    fun stroke(color: Int, widthDp: Float) = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = dp(widthDp)
        this.color = color
        strokeJoin = Paint.Join.MITER
    }

    /** The lens: an outline with cut corners, tick marks, and solid corner wedges. */
    private var frameKey = 0L
    private var framePath = Path()

    fun frame(c: Canvas, w: Float, h: Float) {
        val i = dp(8f)
        val cut = dp(22f)
        val key = (w.toLong() shl 32) or h.toLong()
        if (key != frameKey) {
            frameKey = key
            framePath = lensPath(w, h, i, cut)
        }
        c.drawPath(framePath, chromeDim)
        frameDetails(c, w, h, i)
    }

    private fun lensPath(w: Float, h: Float, i: Float, cut: Float) = Path().apply {
            moveTo(i + cut, i); lineTo(w - i - cut, i); lineTo(w - i, i + cut)
            lineTo(w - i, h - i - cut); lineTo(w - i - cut, h - i); lineTo(i + cut, h - i)
            lineTo(i, h - i - cut); lineTo(i, i + cut); close()
    }

    private fun frameDetails(c: Canvas, w: Float, h: Float, i: Float) {
        // Range ticks along the top and bottom edge, like a scale on the glass.
        val tick = dp(6f)
        for (k in -3..3) {
            val x = w / 2 + k * dp(36f)
            val len = if (k == 0) tick * 2 else tick
            c.drawLine(x, i, x, i + len, chromeDim)
            c.drawLine(x, h - i, x, h - i - len, chromeDim)
        }

        fill.color = CHROME
        val s = dp(9f)
        val o = i + dp(5f)
        wedge(c, o, o, s, s)
        wedge(c, w - o, o, -s, s)
        wedge(c, o, h - o, s, -s)
        wedge(c, w - o, h - o, -s, -s)
    }

    private val wedgePath = Path()

    private fun wedge(c: Canvas, x: Float, y: Float, dx: Float, dy: Float) {
        wedgePath.rewind()
        wedgePath.moveTo(x, y); wedgePath.lineTo(x + dx, y); wedgePath.lineTo(x, y + dy); wedgePath.close()
        c.drawPath(wedgePath, fill)
    }

    /** Reticle corners around [r]: the "target locked" box. */
    fun brackets(c: Canvas, r: RectF, lenDp: Float, p: Paint) {
        val l = dp(lenDp)
        c.drawLine(r.left, r.top, r.left + l, r.top, p); c.drawLine(r.left, r.top, r.left, r.top + l, p)
        c.drawLine(r.right, r.top, r.right - l, r.top, p); c.drawLine(r.right, r.top, r.right, r.top + l, p)
        c.drawLine(r.left, r.bottom, r.left + l, r.bottom, p); c.drawLine(r.left, r.bottom, r.left, r.bottom - l, p)
        c.drawLine(r.right, r.bottom, r.right - l, r.bottom, p); c.drawLine(r.right, r.bottom, r.right, r.bottom - l, p)
    }

    /** The scan line, with a short fading trail above it. */
    private val sweepPaint = stroke(CHROME, 1f)

    fun sweep(c: Canvas, w: Float, y: Float) {
        val p = sweepPaint
        for (k in 0 until 6) {
            p.alpha = 200 - k * 34
            val yy = y - k * dp(3f)
            c.drawLine(0f, yy, w, yy, p)
        }
    }

    private var crackKey = 0L
    private var crackPath = Path()

    /**
     * A shattered lens: jagged rays from an impact point plus two broken rings.
     * Seeded, so it looks the same every time; built once per screen size.
     */
    fun cracks(c: Canvas, w: Float, h: Float, p: Paint) {
        val key = (w.toLong() shl 32) or h.toLong()
        if (key != crackKey) {
            crackKey = key
            crackPath = buildCracks(w, h)
        }
        c.drawPath(crackPath, p)
    }

    private fun buildCracks(w: Float, h: Float): Path {
        val rnd = Random(9000)
        val cx = w * 0.78f
        val cy = h * 0.26f
        val path = Path()
        val rays = 11
        val ringPoints = Array(2) { mutableListOf<Pair<Float, Float>>() }
        for (k in 0 until rays) {
            val angle = (k + rnd.nextFloat() * 0.6f) / rays * 2 * Math.PI
            var x = cx
            var y = cy
            path.moveTo(x, y)
            var dist = 0f
            val reach = w * (0.2f + rnd.nextFloat() * 0.32f)
            var step = 0
            while (dist < reach) {
                val seg = dp(14f + rnd.nextFloat() * 28f)
                val a = angle + (rnd.nextFloat() - 0.5f) * 0.5f
                x += (cos(a) * seg).toFloat()
                y += (sin(a) * seg).toFloat()
                dist += seg
                path.lineTo(x, y)
                if (step == 2) ringPoints[0] += x to y
                if (step == 5) ringPoints[1] += x to y
                step++
            }
        }
        // Rings connect neighbouring rays, with gaps, like real broken glass.
        for (ring in ringPoints) {
            for (k in 0 until ring.size - 1) {
                if (rnd.nextFloat() < 0.3f) continue
                val (x1, y1) = ring[k]
                val (x2, y2) = ring[k + 1]
                path.moveTo(x1, y1)
                path.lineTo((x1 + x2) / 2 + dp((rnd.nextFloat() - 0.5f) * 10), (y1 + y2) / 2 + dp((rnd.nextFloat() - 0.5f) * 10))
                path.lineTo(x2, y2)
            }
        }
        return path
    }

    companion object {
        /** Lens green: darker and bluer than "passing" green so the two never mix up. */
        const val CHROME = 0xB32E9E86.toInt()
        const val CHROME_DIM = 0x732E9E86
        const val CHROME_TEXT = 0xFF45B5A0.toInt()
    }
}
