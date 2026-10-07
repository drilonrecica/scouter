package dev.recica.scouter

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.LinearGradient
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RadialGradient
import android.graphics.Shader
import kotlin.math.sqrt
import kotlin.random.Random

/**
 * The layer behind the HUD: a ki aura in the overall status colour, a sparse
 * star field and a faint hex mesh. Everything stays dim (most pixels remain
 * black, which an AMOLED panel does not light at all).
 *
 * It is painted into a half-resolution bitmap that is rebuilt only when its
 * inputs change, so the scan animation's frames just copy one bitmap. Half
 * resolution is invisible for soft, dim content and saves ~5 MB of RAM and
 * GPU memory (the bitmap and its texture copy).
 */
class Backdrop(private val density: Float) {
    data class Spec(val w: Int, val h: Int, val aura: Int?, val stars: Boolean, val mesh: Boolean, val starSeed: Long)

    private var spec: Spec? = null
    private var bitmap: Bitmap? = null
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val blit = Paint(Paint.FILTER_BITMAP_FLAG)
    private val dst = android.graphics.RectF()

    fun draw(c: Canvas, s: Spec) {
        if (s.w <= 0 || s.h <= 0) return
        if (s != spec) {
            render(s)
            spec = s
        }
        bitmap?.let {
            dst.set(0f, 0f, s.w.toFloat(), s.h.toFloat())
            c.drawBitmap(it, null, dst, blit)
        }
    }

    private fun render(s: Spec) {
        val bw = (s.w + 1) / SCALE
        val bh = (s.h + 1) / SCALE
        val bmp = bitmap?.takeIf { it.width == bw && it.height == bh } ?: Bitmap.createBitmap(bw, bh, Bitmap.Config.ARGB_8888).also {
            bitmap?.recycle()
            bitmap = it
        }
        bmp.eraseColor(0xFF000000.toInt())
        val c = Canvas(bmp)
        c.scale(1f / SCALE, 1f / SCALE) // draw in full-size coordinates
        if (s.mesh) mesh(c, s)
        if (s.stars) stars(c, s)
        s.aura?.let { aura(c, s, it) }
    }

    /** Edge glow: four fading bands and soft corners, max alpha ~0x30. */
    private fun aura(c: Canvas, s: Spec, rgb: Int) {
        val w = s.w.toFloat()
        val h = s.h.toFloat()
        val band = 46 * density
        val strong = (rgb and 0xFFFFFF) or (0x30 shl 24)
        val clear = rgb and 0xFFFFFF
        paint.style = Paint.Style.FILL
        fun rect(x0: Float, y0: Float, x1: Float, y1: Float, gx0: Float, gy0: Float, gx1: Float, gy1: Float) {
            paint.shader = LinearGradient(gx0, gy0, gx1, gy1, strong, clear, Shader.TileMode.CLAMP)
            c.drawRect(x0, y0, x1, y1, paint)
        }
        rect(0f, 0f, w, band, 0f, 0f, 0f, band)
        rect(0f, h - band, w, h, 0f, h, 0f, h - band)
        rect(0f, 0f, band, h, 0f, 0f, band, 0f)
        rect(w - band, 0f, w, h, w, 0f, w - band, 0f)
        val r = band * 2.2f
        for ((x, y) in listOf(0f to 0f, w to 0f, 0f to h, w to h)) {
            paint.shader = RadialGradient(x, y, r, (rgb and 0xFFFFFF) or (0x22 shl 24), clear, Shader.TileMode.CLAMP)
            c.drawCircle(x, y, r, paint)
        }
        paint.shader = null
    }

    /** ~70 dim points; new positions every minute spread pixel wear. */
    private fun stars(c: Canvas, s: Spec) {
        val rnd = Random(s.starSeed)
        paint.style = Paint.Style.FILL
        repeat(70) {
            val bright = rnd.nextInt(25, 91)
            paint.color = (bright shl 24) or 0xDDEEFF
            val r = (if (rnd.nextFloat() < 0.15f) 1.3f * density else 0.7f * density).coerceAtLeast(SCALE.toFloat())
            c.drawCircle(rnd.nextFloat() * s.w, rnd.nextFloat() * s.h, r, paint)
        }
    }

    /** Honeycomb at very low alpha: the texture of the lens glass. */
    private fun mesh(c: Canvas, s: Spec) {
        val size = 14 * density // hex radius
        val hw = sqrt(3f) * size
        val path = Path()
        var row = 0
        var y = 0f
        while (y < s.h + size) {
            var x = if (row % 2 == 0) 0f else hw / 2
            while (x < s.w + hw) {
                for (k in 0..5) {
                    val a = Math.toRadians((60 * k - 30).toDouble())
                    val px = x + size * Math.cos(a).toFloat()
                    val py = y + size * Math.sin(a).toFloat()
                    if (k == 0) path.moveTo(px, py) else path.lineTo(px, py)
                }
                path.close()
                x += hw
            }
            y += size * 1.5f
            row++
        }
        paint.shader = null
        paint.style = Paint.Style.STROKE
        paint.strokeWidth = SCALE.toFloat() // stays one screen pixel after downscaling
        paint.color = 0x142E9E86
        c.drawPath(path, paint)
    }

    private companion object {
        const val SCALE = 2
    }
}
