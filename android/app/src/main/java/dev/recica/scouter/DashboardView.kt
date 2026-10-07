package dev.recica.scouter

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RectF
import android.graphics.Typeface
import android.text.Layout
import android.text.StaticLayout
import android.text.TextPaint
import android.text.TextUtils
import android.view.View
import java.time.Duration
import java.time.Instant

/**
 * Draws every screen with Canvas: no view hierarchy to inflate or measure,
 * and full control over what is lit. AMOLED rules: black background, colour
 * only on accents, nothing full-colour except a transient alert card.
 */
class DashboardView(ctx: Context) : View(ctx) {
    enum class Mode { FOCUS, GRID }

    var mode = Mode.FOCUS
    var pinned: String? = null
    var dismissed: Set<String> = emptySet()

    /** Grid tile rectangles from the last draw, for tap hit-testing. */
    val tiles = mutableListOf<Pair<RectF, Project>>()

    private val d = resources.displayMetrics.density
    private val sp = resources.displayMetrics.scaledDensity
    private fun dp(v: Float) = v * d

    private fun text(sizeSp: Float, color: Int, bold: Boolean = false) = TextPaint(Paint.ANTI_ALIAS_FLAG).apply {
        textSize = sizeSp * sp
        this.color = color
        typeface = if (bold) Typeface.DEFAULT_BOLD else Typeface.DEFAULT
    }

    private val fill = Paint(Paint.ANTI_ALIAS_FLAG)

    override fun onDraw(c: Canvas) {
        c.drawColor(BG)
        tiles.clear()
        val s = Hub.state
        val now = Instant.now()
        when {
            s == null -> centered(c, if (Prefs(context).configured) "Connecting…" else "Not configured — see README")
            freshAlert(s, now) != null -> alertCard(c, freshAlert(s, now)!!, now)
            mode == Mode.GRID -> grid(c, s, now)
            else -> focus(c, s, now)
        }
        if (s != null) {
            banner(c, s, now)
            connection(c, s, now)
        }
    }

    // ---- focus ----------------------------------------------------------------

    private fun focus(c: Canvas, s: DashState, now: Instant) {
        val p = Logic.focus(s, pinned) ?: return centered(c, "No projects")
        val pad = dp(28f)
        val split = width * 0.62f
        val st = Logic.status(p)

        fill.color = color(st)
        c.drawRect(0f, 0f, dp(6f), height.toFloat(), fill)

        var y = pad + dp(30f)
        line(c, p.name, pad, y, split - pad, text(34f, PRIMARY, bold = true))
        line(c, if (pinned == p.fullName) "PINNED" else "AUTO", split + dp(24f), y - dp(6f), width - split, text(16f, DIM, bold = true))

        y += dp(70f)
        line(c, Logic.label(st), pad, y, split - pad, text(54f, color(st), bold = true))
        val ci = p.ci
        if (ci != null) {
            y += dp(36f)
            line(c, "${ci.workflow} · ${ci.branch}", pad, y, split - pad, text(22f, SECONDARY))
            y += dp(16f)
            y = multiline(c, ci.title, pad, y, split - pad * 1.5f, 2, text(26f, PRIMARY)) + dp(30f)
            line(c, listOf(Logic.runTime(ci, now), Logic.ago(ci.startedAt, now)).filter { it.isNotEmpty() }.joinToString(" · "), pad, y, split - pad, text(22f, SECONDARY))
        } else {
            y += dp(36f)
            line(c, "No workflow runs on ${p.defaultBranch}", pad, y, split - pad, text(22f, SECONDARY))
        }

        // Right column: what is happening besides the default branch.
        val x = split + dp(24f)
        val w = width - x - pad
        var ry = pad + dp(100f)
        val other = p.latest
        if (other != null) {
            line(c, "OTHER BRANCH", x, ry, w, text(16f, DIM, bold = true))
            ry += dp(36f)
            line(c, Logic.label(other.status), x, ry, w, text(28f, color(other.status), bold = true))
            ry += dp(30f)
            line(c, other.branch, x, ry, w, text(20f, SECONDARY))
            ry += dp(28f)
            line(c, Logic.runTime(other, now).ifEmpty { Logic.ago(other.startedAt, now) }, x, ry, w, text(20f, SECONDARY))
            ry += dp(44f)
        }
        line(c, "PULL REQUESTS", x, ry, w, text(16f, DIM, bold = true))
        ry += dp(34f)
        line(c, if (p.openPRs == 0) "none open" else "${p.openPRs} open", x, ry, w, text(24f, if (p.openPRs > 0) PRIMARY else SECONDARY))

        // Bottom strip: everything else at a glance.
        val others = s.projects.filter { it.fullName != p.fullName }.take(5)
        if (others.isNotEmpty()) {
            val by = height - pad
            val slot = (width - pad * 2) / others.size
            others.forEachIndexed { i, o ->
                val ox = pad + i * slot
                fill.color = color(Logic.status(o))
                c.drawCircle(ox + dp(6f), by - dp(7f), dp(6f), fill)
                line(c, o.name, ox + dp(18f), by, slot - dp(26f), text(18f, SECONDARY))
            }
        }
    }

    // ---- grid -----------------------------------------------------------------

    private fun grid(c: Canvas, s: DashState, now: Instant) {
        val items = Logic.grid(s)
        if (items.isEmpty()) return centered(c, "No projects")
        val pad = dp(16f)
        val gap = dp(12f)
        val cols = 3
        val rows = 3
        val tw = (width - pad * 2 - gap * (cols - 1)) / cols
        val th = (height - pad * 2 - gap * (rows - 1)) / rows
        val name = text(24f, PRIMARY, bold = true)
        val meta = text(20f, SECONDARY)
        items.forEachIndexed { i, p ->
            val l = pad + (i % cols) * (tw + gap)
            val t = pad + (i / cols) * (th + gap)
            val r = RectF(l, t, l + tw, t + th)
            tiles += r to p
            fill.color = TILE
            c.drawRoundRect(r, dp(10f), dp(10f), fill)
            val st = Logic.status(p)
            fill.color = color(st)
            c.drawRect(l, t + dp(10f), l + dp(6f), t + th - dp(10f), fill)

            val x = l + dp(20f)
            val w = tw - dp(32f)
            // Three lines spread over the tile height, whatever the screen size.
            line(c, p.name, x, t + th * 0.34f, w, name)
            line(c, Logic.label(st), x, t + th * 0.62f, w, text(20f, color(st), bold = true))
            val run = p.ci
            val detail = if (run != null) Logic.runTime(run, now).takeIf { st == Status.RUNNING } ?: Logic.ago(run.startedAt, now) else Logic.ago(p.pushedAt, now)
            line(c, detail, x, t + th * 0.86f, w, meta)
        }
    }

    // ---- alerts ---------------------------------------------------------------

    private fun freshAlert(s: DashState, now: Instant): Alert? = Logic.openAlerts(s, dismissed).firstOrNull { a ->
        val seen = Hub.alertSeenAt[a.id] ?: return@firstOrNull false
        Duration.between(seen, now).seconds < CARD_SECONDS
    }

    private fun alertCard(c: Canvas, a: Alert, now: Instant) {
        c.drawColor(ALERT_BG)
        val pad = dp(40f)
        val w = width - pad * 2
        val project = Hub.state?.projects?.firstOrNull { it.fullName == a.project }?.name ?: a.project
        line(c, "CI FAILED", pad, pad + dp(40f), w, text(28f, 0xFFFFC2C2.toInt(), bold = true))
        line(c, project, pad, pad + dp(110f), w, text(52f, 0xFFFFFFFF.toInt(), bold = true))
        val y = multiline(c, a.text, pad, pad + dp(140f), w, 3, text(26f, 0xFFFFE5E5.toInt()))
        line(c, Logic.ago(a.at, now) + " · tap to dismiss", pad, y + dp(50f), w, text(20f, 0xFFFFC2C2.toInt()))
    }

    /** After the card's minute is up, an undismissed alert lives on as a thin strip. */
    private fun banner(c: Canvas, s: DashState, now: Instant) {
        val open = Logic.openAlerts(s, dismissed)
        if (open.isEmpty() || freshAlert(s, now) != null) return
        val h = dp(40f)
        fill.color = ALERT_BG
        c.drawRect(0f, 0f, width.toFloat(), h, fill)
        val first = open.first()
        val name = s.projects.firstOrNull { it.fullName == first.project }?.name ?: first.project
        val more = if (open.size > 1) "  +${open.size - 1}" else ""
        line(c, "$name: ${first.text}$more", dp(16f), h - dp(12f), width - dp(32f), text(18f, 0xFFFFFFFF.toInt(), bold = true))
    }

    fun bannerHeight() = dp(40f)

    private fun connection(c: Canvas, s: DashState, now: Instant) {
        val msg = when {
            !Hub.connected -> "offline " + Logic.ago(Hub.disconnectedSince, now).removeSuffix(" ago")
            s.sources["github"]?.ok == false -> "GitHub error"
            else -> return
        }
        val p = text(18f, if (!Hub.connected) PURPLE else AMBER, bold = true)
        c.drawText(msg, width - dp(20f) - p.measureText(msg), height - dp(16f), p)
    }

    // ---- text helpers -----------------------------------------------------------

    private fun line(c: Canvas, s: String, x: Float, baseline: Float, maxW: Float, p: TextPaint) {
        if (maxW <= 0) return
        c.drawText(TextUtils.ellipsize(s, p, maxW, TextUtils.TruncateAt.END).toString(), x, baseline, p)
    }

    /** Draws up to [maxLines] lines from [top]; returns the bottom y. */
    private fun multiline(c: Canvas, s: String, x: Float, top: Float, w: Float, maxLines: Int, p: TextPaint): Float {
        val layout = StaticLayout.Builder.obtain(s, 0, s.length, p, w.toInt().coerceAtLeast(1))
            .setAlignment(Layout.Alignment.ALIGN_NORMAL).setMaxLines(maxLines).setEllipsize(TextUtils.TruncateAt.END).build()
        c.save()
        c.translate(x, top)
        layout.draw(c)
        c.restore()
        return top + layout.height
    }

    private fun centered(c: Canvas, s: String) {
        val p = text(24f, SECONDARY)
        c.drawText(s, (width - p.measureText(s)) / 2, height / 2f, p)
    }

    companion object {
        const val CARD_SECONDS = 60L
        private const val BG = 0xFF000000.toInt()
        private const val TILE = 0xFF111111.toInt()
        private const val PRIMARY = 0xFFE8E8E8.toInt()
        private const val SECONDARY = 0xFF8C8C8C.toInt()
        private const val DIM = 0xFF5A5A5A.toInt()
        private const val GREEN = 0xFF30D158.toInt()
        private const val RED = 0xFFFF453A.toInt()
        private const val AMBER = 0xFFFFB340.toInt()
        private const val GRAY = 0xFF6E6E73.toInt()
        private const val PURPLE = 0xFFBF5AF2.toInt()
        private const val ALERT_BG = 0xFF8B0A0A.toInt()

        fun color(s: Status) = when (s) {
            Status.SUCCESS -> GREEN
            Status.FAILURE -> RED
            Status.RUNNING -> AMBER
            Status.CANCELLED, Status.NONE -> GRAY
        }
    }
}
