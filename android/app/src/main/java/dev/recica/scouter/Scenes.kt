package dev.recica.scouter

import android.graphics.Canvas
import android.graphics.Color
import android.graphics.DashPathEffect
import android.graphics.LinearGradient
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RadialGradient
import android.graphics.RectF
import android.graphics.Shader
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.cos
import kotlin.math.roundToInt
import kotlin.math.sin

/** The places a project's builds are fought: original drawings, no official artwork. */
enum class Stage { NAMEK, WASTELAND, TOURNAMENT, LOOKOUT, POWER }

/**
 * Draws the stage scenes, the Dragon Balls wish and the status weather, in
 * the landscape screen's 640×360 dp coordinates (the caller scales). Ported
 * from the backdrop sketchboard; colours are stored dim and the caller lifts
 * the whole scene to the chosen brightness. Allocates freely: it runs only
 * when the backdrop is rebuilt, not per frame.
 */
object Scenes {
    const val W = 640f
    const val H = 360f

    /** Where the ground is, for the weather's ki sparks and cracks. */
    fun ground(stage: Stage?): Float = when (stage) {
        Stage.NAMEK, Stage.TOURNAMENT, Stage.POWER -> 250f
        Stage.WASTELAND, Stage.LOOKOUT -> 270f
        null -> 300f
    }

    private val p = Paint(Paint.ANTI_ALIAS_FLAG)

    fun draw(c: Canvas, stage: Stage, hour: Int) {
        when (stage) {
            Stage.NAMEK -> namek(c, hour)
            Stage.WASTELAND -> wasteland(c, hour)
            Stage.TOURNAMENT -> tournament(c, hour)
            Stage.LOOKOUT -> lookout(c, hour)
            Stage.POWER -> power(c, hour)
        }
        reset()
    }

    // ---- helpers --------------------------------------------------------------------

    /** mulberry32, as in the sketchboard, so a seed gives the same scene. */
    private class Rng(private var s: Int) {
        fun next(): Float {
            s += 0x6D2B79F5
            var t = (s xor (s ushr 15)) * (1 or s)
            t = (t + (t xor (t ushr 7)) * (61 or t)) xor t
            return ((t xor (t ushr 14)).toLong() and 0xFFFFFFFFL).toFloat() / 4294967296f
        }
    }

    private fun rgba(r: Int, g: Int, b: Int, a: Float) = Color.argb((a * 255).roundToInt().coerceIn(0, 255), r, g, b)
    private fun hex(v: Int) = v or (0xFF shl 24)
    private fun clear(color: Int) = color and 0x00FFFFFF

    private fun mix(a: Int, b: Int, t: Float): Int {
        fun ch(s: Int) = (((a shr s) and 255) * (1 - t) + ((b shr s) and 255) * t).roundToInt()
        return Color.rgb(ch(16), ch(8), ch(0))
    }

    private fun reset() {
        p.shader = null
        p.pathEffect = null
        p.style = Paint.Style.FILL
        p.strokeCap = Paint.Cap.BUTT
    }

    private fun fill(color: Int) { reset(); p.color = color }
    private fun stroke(color: Int, w: Float) { reset(); p.style = Paint.Style.STROKE; p.color = color; p.strokeWidth = w }

    private fun sky(c: Canvas, top: Int, horizon: Int, y: Float) {
        reset()
        p.shader = LinearGradient(0f, 0f, 0f, y, top, horizon, Shader.TileMode.CLAMP)
        c.drawRect(0f, 0f, W, y + 1, p)
    }

    private fun glow(c: Canvas, x: Float, y: Float, r: Float, color: Int) {
        reset()
        p.shader = RadialGradient(x, y, r, color, clear(color), Shader.TileMode.CLAMP)
        c.drawCircle(x, y, r, p)
    }

    private fun disc(c: Canvas, x: Float, y: Float, r: Float, color: Int) { fill(color); c.drawCircle(x, y, r, p) }

    private fun shape(c: Canvas, pts: List<Pair<Float, Float>>, fillColor: Int, rim: Int?, rimW: Float = 1.2f) {
        val path = Path()
        pts.forEachIndexed { i, (x, y) -> if (i == 0) path.moveTo(x, y) else path.lineTo(x, y) }
        path.close()
        fill(fillColor); c.drawPath(path, p)
        rim?.let { stroke(it, rimW); c.drawPath(path, p) }
    }

    private fun stars(c: Canvas, seed: Int, n: Int, maxA: Float) {
        val r = Rng(seed)
        repeat(n) {
            val x = r.next() * W
            val y = r.next() * H * 0.7f
            val size = if (r.next() < .15f) 1.1f else .6f
            disc(c, x, y, size, rgba(221, 238, 255, (.1f + r.next() * .9f) * maxA))
        }
    }

    /** The hour tints every sky: night, dawn, day, dusk. Dark values only. */
    private fun tint(hour: Int): Int {
        val keys = listOf(0 to 0x000000, 5 to 0x000000, 6 to 0x1a0d1c, 7 to 0x20120a, 9 to 0x07121e, 16 to 0x07121e,
            18 to 0x22100a, 19 to 0x1a0a14, 21 to 0x000000, 24 to 0x000000)
        for (i in 0 until keys.size - 1) {
            val (h0, c0) = keys[i]
            val (h1, c1) = keys[i + 1]
            if (hour >= h0 && hour < h1) return mix(c0, c1, (hour - h0).toFloat() / (h1 - h0))
        }
        return Color.BLACK
    }

    private fun night(hour: Int) = hour >= 21 || hour < 6

    // ---- Namek: three suns, rock islands with round-topped trees, still water ----------------

    private fun pillar(c: Canvas, x: Float, base: Float, w: Float, h: Float, fillColor: Int, rim: Int) {
        val top = base - h
        shape(c, listOf(x - w * .35f to base, x - w * .28f to top + 14, x - w * .55f to top + 4, x - w * .5f to top,
            x + w * .5f to top, x + w * .55f to top + 4, x + w * .28f to top + 14, x + w * .35f to base), fillColor, rim)
    }

    private fun tree(c: Canvas, x: Float, y: Float, h: Float, fillColor: Int, rim: Int) {
        stroke(fillColor, 1.6f); c.drawLine(x, y, x, y - h, p)
        for ((dx, dy, r) in listOf(Triple(0f, -h - 6, 7f), Triple(-6f, -h - 1, 5f), Triple(6f, -h - 2, 5f))) {
            fill(fillColor); c.drawCircle(x + dx, y + dy, r, p)
            stroke(rim, .8f); c.drawCircle(x + dx, y + dy, r, p)
        }
    }

    private fun namek(c: Canvas, hour: Int) {
        val t = tint(hour)
        sky(c, mix(Color.BLACK, t, .6f), mix(hex(0x0b2a1a), t, .3f), 250f)
        for ((x, y, r) in listOf(Triple(110f, 78f, 9f), Triple(292f, 46f, 6f), Triple(528f, 96f, 11f))) {
            glow(c, x, y, r * 5, rgba(190, 255, 200, .07f))
            disc(c, x, y, r, rgba(225, 255, 215, .20f))
        }
        reset()
        p.shader = LinearGradient(0f, 250f, 0f, H, hex(0x081a11), Color.BLACK, Shader.TileMode.CLAMP)
        c.drawRect(0f, 250f, W, H, p)
        val r = Rng(3)
        stroke(rgba(130, 230, 180, .07f), 1f)
        repeat(40) {
            val x = r.next() * W
            val y = 256 + r.next() * 100
            val l = 8 + r.next() * 30
            c.drawLine(x, y, x + l, y, p)
        }
        val farFill = hex(0x0a1f15)
        val farRim = rgba(60, 140, 100, .16f)
        for ((x, h, wd) in listOf(Triple(70f, 70f, 34f), Triple(250f, 50f, 26f), Triple(400f, 90f, 40f), Triple(590f, 60f, 30f))) {
            pillar(c, x, 252f, wd, h, farFill, farRim)
            tree(c, x - 6, 252 - h, 12f, farFill, farRim)
            tree(c, x + 8, 252 - h, 16f, farFill, farRim)
        }
        val nearFill = hex(0x04100a)
        val nearRim = rgba(70, 170, 120, .22f)
        for ((x, h, wd) in listOf(Triple(160f, 150f, 60f), Triple(505f, 125f, 54f))) {
            pillar(c, x, 262f, wd, h, nearFill, nearRim)
            tree(c, x - 12, 262 - h, 20f, nearFill, nearRim)
            tree(c, x + 10, 262 - h, 28f, nearFill, nearRim)
        }
    }

    // ---- Wasteland: dusky plateaus, boulders and a crater -----------------------------------

    private fun mesas(c: Canvas, seed: Int, base: Float, amp: Float, fillColor: Int, rim: Int) {
        val r = Rng(seed)
        val pts = mutableListOf(0f to H)
        var x = 0f
        while (x < W + 40) {
            val plateau = r.next() < .45f
            val nx = x + 30 + r.next() * 70
            val ny = if (plateau) base - amp * (.5f + r.next() * .5f) else base - r.next() * amp * .3f
            pts += x + 10 to ny
            pts += nx - 10 to ny
            x = nx
        }
        pts += W to H
        shape(c, pts, fillColor, rim)
    }

    private fun wasteland(c: Canvas, hour: Int) {
        val t = tint(hour)
        sky(c, mix(Color.BLACK, t, .6f), mix(hex(0x1c0f07), t, .3f), 235f)
        glow(c, 470f, 230f, 90f, rgba(255, 140, 60, .08f))
        disc(c, 470f, 230f, 26f, rgba(255, 150, 80, .10f))
        mesas(c, 11, 245f, 60f, hex(0x120a05), rgba(160, 90, 40, .10f))
        mesas(c, 12, 275f, 70f, hex(0x0b0603), rgba(170, 100, 50, .15f))
        fill(hex(0x050302)); c.drawRect(0f, 300f, W, H, p)
        val r = Rng(13)
        repeat(9) {
            val x = r.next() * W
            val y = 304 + r.next() * 46
            val rx = 6 + r.next() * 22
            val oval = RectF(x - rx, y - rx * .45f, x + rx, y + rx * .45f)
            fill(hex(0x0a0503)); c.drawArc(oval, 180f, 180f, false, p)
            stroke(rgba(180, 110, 60, .14f), 1f); c.drawArc(oval, 180f, 180f, false, p)
        }
        stroke(rgba(180, 110, 60, .10f), 1f)
        c.drawOval(RectF(90f, 323f, 230f, 347f), p)
    }

    // ---- Tournament: a stone ring before a curved-roof temple and palms ---------------------------

    private fun roof(c: Canvas, cx: Float, y: Float, w: Float, h: Float, fillColor: Int, rim: Int) {
        val path = Path().apply {
            moveTo(cx - w, y)
            quadTo(cx - w * .55f, y - h * .3f, cx - w * .3f, y - h)
            lineTo(cx + w * .3f, y - h)
            quadTo(cx + w * .55f, y - h * .3f, cx + w, y)
            close()
        }
        fill(fillColor); c.drawPath(path, p)
        stroke(rim, 1.2f); c.drawPath(path, p)
    }

    private fun palm(c: Canvas, x: Float, base: Float, h: Float, lean: Float, rim: Int) {
        stroke(hex(0x05080f), 3f)
        c.drawPath(Path().apply { moveTo(x, base); quadTo(x + lean * .5f, base - h * .6f, x + lean, base - h) }, p)
        stroke(rim, 2f)
        for (k in 0 until 6) {
            val a = PI + k * PI / 5
            val tx = x + lean
            val ty = base - h
            c.drawPath(Path().apply {
                moveTo(tx, ty)
                quadTo(tx + cos(a).toFloat() * 18, ty - 10, tx + cos(a).toFloat() * 30, ty + 8 + abs(sin(a)).toFloat() * 6)
            }, p)
        }
    }

    private fun tournament(c: Canvas, hour: Int) {
        val t = tint(hour)
        sky(c, mix(Color.BLACK, t, .6f), mix(hex(0x0a1222), t, .3f), 215f)
        if (night(hour)) stars(c, 21, 40, .25f)
        val stone = hex(0x070b14)
        val rim = rgba(70, 100, 160, .20f)
        fill(stone); c.drawRect(250f, 168f, 390f, 218f, p)
        roof(c, 320f, 172f, 110f, 26f, stone, rim)
        roof(c, 320f, 140f, 70f, 22f, stone, rim)
        val frond = rgba(70, 100, 160, .18f)
        palm(c, 70f, 250f, 90f, 12f, frond)
        palm(c, 110f, 250f, 70f, -10f, frond)
        palm(c, 560f, 250f, 95f, -14f, frond)
        fill(hex(0x03050a)); c.drawRect(0f, 248f, W, H, p)
        shape(c, listOf(175f to 258f, 465f to 258f, 590f to 352f, 50f to 352f), hex(0x0c0f16), rgba(150, 170, 210, .20f), 1.4f)
        stroke(rgba(150, 170, 210, .06f), 1f)
        for (i in 1 until 8) {
            val k = i / 8f
            c.drawLine(175 + 290 * k, 258f, 50 + 540 * k, 352f, p)
        }
        for (i in 1 until 5) {
            val k = i / 5f
            val y = 258 + 94 * k * k * .9f + 94 * k * .1f
            val f = (y - 258) / 94
            c.drawLine(175 - 125 * f, y, 465 + 125 * f, y, p)
        }
    }

    // ---- Lookout: the floating platform above a sea of clouds ------------------------------------

    private fun lookout(c: Canvas, hour: Int) {
        val t = tint(hour)
        sky(c, mix(Color.BLACK, t, .6f), mix(hex(0x081428), t, .3f), 270f)
        if (night(hour)) stars(c, 31, 50, .3f)
        val r = Rng(32)
        repeat(26) {
            val x = r.next() * W
            val y = 268 + r.next() * 80
            val rx = 40 + r.next() * 70
            c.save()
            c.translate(x, y)
            c.scale(1f, .35f)
            reset()
            p.shader = RadialGradient(0f, 0f, rx, rgba(130, 160, 215, .10f), 0, Shader.TileMode.CLAMP)
            c.drawCircle(0f, 0f, rx, p)
            c.restore()
        }
        val cx = 430f
        val y = 150f
        val rw = 130f
        val body = hex(0x060a14)
        val rim = rgba(90, 120, 190, .22f)
        fill(body); c.drawOval(RectF(cx - rw, y - 12, cx + rw, y + 12), p)
        val under = Path().apply {
            moveTo(cx - rw, y)
            quadTo(cx - rw * .6f, y + 70, cx, y + 78)
            quadTo(cx + rw * .6f, y + 70, cx + rw, y)
            close()
        }
        val spire = Path().apply { moveTo(cx - 6, y + 76); lineTo(cx, y + 118); lineTo(cx + 6, y + 76) }
        for (path in listOf(under, spire)) {
            fill(body); c.drawPath(path, p)
            stroke(rim, 1.2f); c.drawPath(path, p)
        }
        for ((dx, rr) in listOf(0f to 34f, -62f to 12f, 62f to 12f)) {
            val oval = RectF(cx + dx - rr, y - (if (dx == 0f) 6 else 4) - rr, cx + dx + rr, y - (if (dx == 0f) 6 else 4) + rr)
            fill(body); c.drawArc(oval, 180f, 180f, true, p)
            stroke(rim, 1.2f); c.drawArc(oval, 180f, 180f, false, p)
        }
        stroke(rim, 1.2f); c.drawLine(cx, y - 40, cx, y - 54, p)
        stroke(rim, 2f)
        for (px in listOf(cx - 105, cx + 100)) {
            c.drawLine(px, y - 2, px + 3, y - 34, p)
            for (k in 0 until 5) c.drawLine(px + 3, y - 34, px + 3 + (k - 2) * 9, y - 28 + abs(k - 2) * 3, p)
        }
    }

    // ---- Tournament of Power: a broken arena and rubble in the void --------------------------------

    private fun power(c: Canvas, hour: Int) {
        reset()
        p.shader = RadialGradient(320f, 200f, 420f, mix(hex(0x140b22), tint(hour), .2f), Color.BLACK, Shader.TileMode.CLAMP)
        c.drawRect(0f, 0f, W, H, p)
        stars(c, 41, 90, .3f)
        val rim = rgba(170, 120, 240, .22f)
        for ((x, y, rr) in listOf(Triple(90f, 70f, 30f), Triple(560f, 60f, 18f))) {
            disc(c, x, y, rr, hex(0x08050f))
            stroke(rgba(150, 110, 220, .18f), 1f); c.drawCircle(x, y, rr, p)
        }
        val arena = Path().apply {
            fillType = Path.FillType.EVEN_ODD
            addOval(RectF(60f, 210f, 580f, 314f), Path.Direction.CW)
            moveTo(440f, 214f); lineTo(500f, 250f); lineTo(470f, 300f); lineTo(410f, 310f); lineTo(430f, 262f); close()
        }
        fill(hex(0x0a0712)); c.drawPath(arena, p)
        stroke(rim, 1.3f); c.drawPath(arena, p)
        fill(hex(0x050309)); c.drawArc(RectF(62f, 224f, 578f, 328f), 0f, 180f, false, p)
        for ((x, h) in listOf(120f to 40f, 200f to 52f, 440f to 50f, 520f to 38f)) {
            val rect = RectF(x - 4, 262 - h - 20, x + 4, 262 - 20f)
            fill(hex(0x0a0712)); c.drawRect(rect, p)
            stroke(rgba(170, 120, 240, .18f), 1f); c.drawRect(rect, p)
        }
        val r = Rng(42)
        repeat(9) {
            val x = r.next() * W
            val y = 120 + r.next() * 220
            val s = 6 + r.next() * 16
            val pts = (0 until 5).map { k ->
                val a = k / 5f * 2 * PI.toFloat() + r.next()
                x + cos(a) * s * (.6f + r.next() * .6f) to y + sin(a) * s * .6f
            }
            shape(c, pts, hex(0x0a0712), rgba(170, 120, 240, .20f))
        }
    }

    // ---- the wish: all seven balls lit and the eternal dragon --------------------------------------

    fun wish(c: Canvas) {
        reset()
        p.shader = LinearGradient(0f, 0f, 0f, H, hex(0x06040e), Color.BLACK, Shader.TileMode.CLAMP)
        c.drawRect(0f, 0f, W, H, p)
        val body = Path().apply {
            moveTo(60f, 340f)
            cubicTo(140f, 180f, 260f, 330f, 330f, 200f)
            cubicTo(390f, 90f, 520f, 180f, 560f, 90f)
        }
        stroke(rgba(60, 170, 100, .20f), 30f); p.strokeCap = Paint.Cap.ROUND; c.drawPath(body, p)
        stroke(hex(0x04110a), 26f); p.strokeCap = Paint.Cap.ROUND; c.drawPath(body, p)
        stroke(rgba(60, 170, 100, .10f), 1f); p.pathEffect = DashPathEffect(floatArrayOf(3f, 6f), 0f); c.drawPath(body, p)
        shape(c, listOf(548f to 78f, 600f to 66f, 612f to 82f, 588f to 96f, 560f to 104f), hex(0x04110a), rgba(60, 170, 100, .24f))
        disc(c, 590f, 78f, 2.2f, rgba(255, 80, 60, .45f))
        stroke(rgba(60, 170, 100, .22f), 1.2f)
        c.drawPath(Path().apply { moveTo(566f, 72f); quadTo(560f, 40f, 540f, 30f); moveTo(578f, 68f); quadTo(586f, 36f, 606f, 26f) }, p)
        val balls = listOf(120f to 300f, 190f to 318f, 262f to 306f, 330f to 322f, 400f to 304f, 468f to 318f, 536f to 300f)
        balls.forEachIndexed { i, (x, y) ->
            val r = 15f
            glow(c, x, y, r * 2.6f, rgba(255, 190, 80, .16f))
            reset()
            p.shader = RadialGradient(x - 5, y - 5, r, rgba(255, 190, 90, .34f), rgba(150, 70, 0, .26f), Shader.TileMode.CLAMP)
            c.drawCircle(x, y, r, p)
            fill(rgba(255, 60, 40, .42f))
            for (s in 0..i) {
                val a = s.toFloat() / (i + 1) * 2 * PI.toFloat()
                val d = if (i == 0) 0f else 5f
                star(c, x + cos(a) * d, y + sin(a) * d, 2.4f)
            }
        }
        reset()
    }

    private fun star(c: Canvas, x: Float, y: Float, r: Float) {
        val path = Path()
        for (k in 0 until 10) {
            val a = -PI / 2 + k * PI / 5
            val rr = if (k % 2 == 1) r * .45f else r
            val px = x + cos(a).toFloat() * rr
            val py = y + sin(a).toFloat() * rr
            if (k == 0) path.moveTo(px, py) else path.lineTo(px, py)
        }
        path.close()
        c.drawPath(path, p)
    }

    // ---- status weather ---------------------------------------------------------------------------

    /** Weather over the stage: sparks while building, a storm on fresh failures, static without a link. */
    fun weather(c: Canvas, led: Logic.Led, ground: Float) {
        val r = Rng(77)
        when (led) {
            Logic.Led.GREEN -> Unit // calm
            Logic.Led.AMBER -> repeat(46) {
                val x = r.next() * W
                val y = ground - r.next() * 200
                val l = 4 + r.next() * 12
                stroke(rgba(255, 179, 64, .05f + r.next() * .12f), 1.2f)
                c.drawLine(x, y, x + (r.next() - .5f) * 2, y - l, p)
            }
            Logic.Led.RED -> {
                fun bolt(x0: Float, y0: Float, len: Float, w: Float): Pair<Float, Float> {
                    val path = Path().apply { moveTo(x0, y0) }
                    var x = x0
                    var y = y0
                    while (y < y0 + len) {
                        x += (r.next() - .5f) * 26
                        y += 10 + r.next() * 16
                        path.lineTo(x, y)
                    }
                    stroke(rgba(255, 120, 110, .07f), w * 5); c.drawPath(path, p)
                    stroke(rgba(255, 170, 160, .20f), w); c.drawPath(path, p)
                    return x to y
                }
                val (bx, by) = bolt(470f, 0f, 150f, 1.4f)
                bolt(bx, by - 40, 50f, .8f)
                val crack = Path()
                var x = 0f
                crack.moveTo(x, ground + 18)
                while (x < W) {
                    x += 14 + r.next() * 24
                    crack.lineTo(x, ground + 12 + r.next() * 16)
                }
                stroke(rgba(255, 69, 58, .07f), 8f); c.drawPath(crack, p)
                stroke(rgba(255, 90, 80, .22f), 1.2f); c.drawPath(crack, p)
            }
            Logic.Led.PURPLE -> {
                repeat(900) {
                    val a = r.next() * .08f
                    fill(rgba(176, 107, 255, a))
                    val x = r.next() * W
                    val y = r.next() * H
                    c.drawRect(x, y, x + 1.2f, y + 1.2f, p)
                }
                fill(rgba(176, 107, 255, .025f))
                for (y in listOf(80f, 190f, 300f)) c.drawRect(0f, y, W, y + 6, p)
            }
            Logic.Led.BLUE -> {
                reset()
                p.shader = LinearGradient(0f, 0f, 0f, 120f, rgba(77, 163, 255, .08f), 0, Shader.TileMode.CLAMP)
                c.drawRect(0f, 0f, W, 120f, p)
            }
        }
        reset()
    }
}
