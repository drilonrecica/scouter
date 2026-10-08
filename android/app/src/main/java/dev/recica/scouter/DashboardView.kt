package dev.recica.scouter

import android.animation.ValueAnimator
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
import android.view.animation.DecelerateInterpolator
import java.time.Duration
import java.time.Instant
import kotlin.math.roundToInt

/**
 * Draws every screen with Canvas as a Scouter HUD: no view hierarchy to
 * inflate or measure, and full control over what is lit. AMOLED rules: black
 * background, chrome as thin lines, colour only on accents, nothing
 * full-colour except a transient alert card, animation only on change.
 */
class DashboardView(ctx: Context) : View(ctx) {
    enum class Mode { FOCUS, GRID, AGENTS }

    var mode = Mode.FOCUS
    var dismissed: Set<String> = emptySet()

    /** Grid tile rectangles from the last draw, for tap hit-testing. */
    val tiles = mutableListOf<Pair<RectF, Project>>()

    private val d = resources.displayMetrics.density
    private val sp = resources.displayMetrics.scaledDensity
    private fun dp(v: Float) = v * d

    private val hud = Hud(d)
    private val backdrop = Backdrop(d)
    private val mono: Typeface = resources.getFont(R.font.share_tech_mono)

    // Paints and layouts are cached: the scan animation redraws every frame,
    // and allocating them per draw made every frame miss on this hardware.
    private val paints = HashMap<Triple<Float, Int, Typeface>, TextPaint>()

    private fun paint(sizeSp: Float, color: Int, face: Typeface) = paints.getOrPut(Triple(sizeSp, color, face)) {
        TextPaint(Paint.ANTI_ALIAS_FLAG).apply {
            textSize = sizeSp * sp
            this.color = color
            typeface = face
        }
    }

    private fun text(sizeSp: Float, color: Int, bold: Boolean = false) = paint(sizeSp, color, if (bold) Typeface.DEFAULT_BOLD else Typeface.DEFAULT)

    private fun monoText(sizeSp: Float, color: Int = Hud.CHROME_TEXT) = paint(sizeSp, color, mono)

    private val strokes = HashMap<Pair<Int, Float>, Paint>()

    private fun stroke(color: Int, widthDp: Float) = strokes.getOrPut(color to widthDp) { hud.stroke(color, widthDp) }

    private val layouts = object : LinkedHashMap<String, StaticLayout>(16, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, StaticLayout>) = size > 16
    }

    private val fill = Paint(Paint.ANTI_ALIAS_FLAG)

    // ---- scan animation ---------------------------------------------------------

    /** 0..1 while scanning, 1 when idle: nothing animates between changes. */
    private var scan = 1f
    private var counting: Map<String, Pair<Int, Int>> = emptyMap()

    /** Power each project had before its last change, for "9000 → 6750" on alerts. */
    private val previousPower = mutableMapOf<String, Int>()

    private val scanner = ValueAnimator.ofFloat(0f, 1f).apply {
        duration = 900
        interpolator = DecelerateInterpolator()
        addUpdateListener {
            scan = it.animatedValue as Float
            invalidate()
        }
    }

    /** The scan line lives in its own overlay so a sweep never redraws the whole HUD. */
    var sweep: SweepView? = null

    /**
     * Called when a new document arrives: redraw once, sweep the lens on the
     * overlay, and only animate this view when a power level must count.
     */
    fun onStateChanged(old: DashState?, new: DashState) {
        val changes = Logic.powerChanges(old, new)
        changes.forEach { (name, v) -> previousPower[name] = v.first }
        previousPower.keys.retainAll(new.projects.map { it.fullName }.toSet())
        counting = changes
        invalidate()
        if (old == null) return
        if (Logic.freshAlertVisible(new, dismissed).not()) sweep?.scan()
        if (changes.isNotEmpty()) {
            scanner.cancel()
            scanner.start()
        }
    }

    private fun shownPower(p: Project): Int? =
        counting[p.fullName]?.takeIf { scan < 1f }?.let { (from, to) -> (from + (to - from) * scan).roundToInt() } ?: p.power

    override fun onDetachedFromWindow() {
        scanner.cancel()
        super.onDetachedFromWindow()
    }

    // ---- frame ------------------------------------------------------------------

    override fun onDraw(c: Canvas) {
        c.drawColor(BG)
        tiles.clear()
        val s = Hub.state
        val now = Instant.now()
        val w = width.toFloat()
        val h = height.toFloat()
        val alert = s?.let { freshAlert(it, now) }
        when {
            s == null -> {
                hud.frame(c, w, h)
                centered(c, if (Prefs(context).configured) "SCOUTER ONLINE · CONNECTING…" else "NOT CONFIGURED · SEE README")
            }
            alert != null -> alertCard(c, alert, now)
            briefingShown(now) -> { background(c, s, now); hud.frame(c, w, h); briefing(c, s, now) }
            mode == Mode.GRID -> { background(c, s, now); hud.frame(c, w, h); grid(c, s, now) }
            mode == Mode.AGENTS -> { background(c, s, now); hud.frame(c, w, h); agents(c, s, now) }
            else -> { background(c, s, now); hud.frame(c, w, h); focus(c, s, now) }
        }
        if (s != null) banner(c, s, now)
    }

    // ---- backdrop -----------------------------------------------------------------

    /** Aura colour = the LED's verdict, so screen and light always agree. */
    private fun background(c: Canvas, s: DashState, now: Instant) {
        val bg = s.settings
        val aura = if (!bg.aura) null else when (Logic.led(s, Hub.connected, dismissed)) {
            Logic.Led.GREEN -> GREEN
            Logic.Led.AMBER -> AMBER
            Logic.Led.RED -> RED
            Logic.Led.BLUE -> BLUE
            Logic.Led.PURPLE -> PURPLE
        }
        // The view is pixel-shifted; the backdrop covers that margin too.
        c.save()
        c.translate(-translationX, -translationY)
        backdrop.draw(c, Backdrop.Spec(width, height, aura, bg.stars, bg.mesh, now.epochSecond / 60))
        c.restore()
    }

    // ---- focus ------------------------------------------------------------------

    private fun focus(c: Canvas, s: DashState, now: Instant) {
        val p = Logic.focus(s) ?: return centered(c, "NO TARGETS")
        val pad = dp(32f)
        val w = width.toFloat()
        val split = w * 0.62f
        val st = Logic.status(p)

        // Header: what we're looking at, and how strong it is.
        val top = dp(48f)
        val scanLabel = monoText(16f)
        c.drawText("SCAN ▸", pad, top, scanLabel)
        val nameX = pad + scanLabel.measureText("SCAN ▸ ")
        val pwr = monoText(30f, PRIMARY)
        val pwrText = Logic.powerText(shownPower(p))
        val pwrX = w - pad - pwr.measureText(pwrText)
        c.drawText(pwrText, pwrX, top, pwr)
        val pwrLabel = monoText(16f)
        c.drawText("PWR", pwrX - dp(10f) - pwrLabel.measureText("PWR"), top, pwrLabel)
        val nameText = text(30f, PRIMARY, bold = true)
        val nameIcon = icon(c, p, nameX, top, 30f, nameText)
        line(c, p.name, nameX + nameIcon, top, pwrX - nameX - dp(70f) - nameIcon, nameText)
        s.history[p.fullName]?.let { series ->
            sparkline(c, series, RectF(w - pad - dp(150f), top + dp(10f), w - pad, top + dp(30f)), color(st))
        }

        // Target box around the status: the brackets take the status colour.
        val box = RectF(pad, dp(72f), split - dp(8f), dp(282f))
        hud.brackets(c, box, 18f, stroke(color(st), 2f))
        val x = box.left + dp(22f)
        val bw = box.width() - dp(40f)
        var y = box.top + dp(64f)
        line(c, Logic.label(st), x, y, bw, text(52f, color(st), bold = true))
        val ci = p.ci
        if (ci != null) {
            y += dp(34f)
            val meta = listOf("${ci.workflow} · ${ci.branch}", Logic.runTime(ci, now), Logic.ago(ci.startedAt, now)).filter { it.isNotEmpty() }
            line(c, meta.joinToString(" · "), x, y, bw, text(20f, SECONDARY))
            y += dp(14f)
            multiline(c, ci.title, x, y, bw, 2, text(24f, PRIMARY))
        } else {
            y += dp(34f)
            line(c, "No workflow runs on ${p.defaultBranch}", x, y, bw, text(20f, SECONDARY))
        }

        if (p.mismatch) line(c, "⚠ DEPLOYED WHILE CI RED", x, box.bottom - dp(14f), bw, monoText(16f, RED))

        // Right column: tracking mode, other branches, deploy, pull requests.
        val rx = split + dp(20f)
        val rw = w - rx - pad
        var ry = dp(96f)
        val tracking = when {
            Logic.locked(s) -> "◉ LOCKED"
            s.settings.focusMode == "rotate" -> "↻ ROTATING"
            else -> "◎ TRACKING"
        }
        val issue = connectionIssue(s, now)
        line(c, issue?.first ?: tracking, rx, ry, rw, monoText(15f, issue?.second ?: Hud.CHROME_TEXT))
        ry += dp(40f)
        val other = p.latest
        if (other != null) {
            c.drawText("OTHER BRANCH", rx, ry, monoText(15f))
            ry += dp(30f)
            line(c, Logic.label(other.status), rx, ry, rw, text(26f, color(other.status), bold = true))
            ry += dp(26f)
            line(c, other.branch + " · " + Logic.runTime(other, now).ifEmpty { Logic.ago(other.startedAt, now) }, rx, ry, rw, text(18f, SECONDARY))
            ry += dp(40f)
        }
        p.deploy?.let { d ->
            c.drawText(if (d.apps > 1) "DEPLOY · ${d.apps} APPS" else "DEPLOY", rx, ry, monoText(15f))
            ry += dp(30f)
            line(c, Logic.deployLabel(d.status), rx, ry, rw, text(26f, color(d.status), bold = true))
            ry += dp(26f)
            val meta = listOf(d.commit.take(7), Logic.ago(d.at, now), Logic.health(d.health).takeIf { it != "healthy" }.orEmpty()).filter { it.isNotEmpty() }
            line(c, meta.joinToString(" · "), rx, ry, rw, text(18f, SECONDARY))
            ry += dp(40f)
        }
        c.drawText("PULL REQUESTS", rx, ry, monoText(15f))
        ry += dp(30f)
        line(c, if (p.openPRs == 0) "none open" else "${p.openPRs} open", rx, ry, rw, text(22f, if (p.openPRs > 0) PRIMARY else SECONDARY))


        // Bottom strip: everything else at a glance.
        val waiting = Logic.waiting(s).size
        val others = s.projects.filter { it.fullName != p.fullName }.take(if (waiting > 0) 4 else 5)
        if (others.isNotEmpty() || waiting > 0) {
            val by = height - dp(30f)
            val slots = others.size + if (waiting > 0) 1 else 0
            val slot = (w - pad * 2) / slots
            if (waiting > 0) line(c, "◆ $waiting WAITING", pad, by, slot - dp(8f), monoText(17f, BLUE))
            val first = if (waiting > 0) 1 else 0
            others.forEachIndexed { idx, o ->
                val i = idx + first
                val ox = pad + i * slot
                fill.color = color(Logic.status(o))
                c.drawCircle(ox + dp(5f), by - dp(6f), dp(5f), fill)
                line(c, o.name, ox + dp(16f), by, slot - dp(24f), text(17f, SECONDARY))
            }
        }
    }

    /** Power over 14 days: chrome line, last point in the status colour, gaps where unknown. */
    private fun sparkline(c: Canvas, series: List<Int>, r: RectF, last: Int) {
        if (series.count { it >= 0 } < 2) return
        val stepX = r.width() / (series.size - 1)
        fun y(v: Int) = r.bottom - r.height() * v / Logic.MAX_POWER.toFloat()
        val line = stroke(Hud.CHROME, 1.5f)
        c.drawLine(r.left, r.bottom, r.right, r.bottom, stroke(Hud.CHROME_DIM, 1f))
        for (i in 1 until series.size) {
            val a = series[i - 1]
            val b = series[i]
            if (a >= 0 && b >= 0) c.drawLine(r.left + (i - 1) * stepX, y(a), r.left + i * stepX, y(b), line)
        }
        series.lastOrNull()?.takeIf { it >= 0 }?.let {
            fill.color = last
            c.drawCircle(r.right, y(it), dp(3.5f), fill)
        }
    }

    // ---- agents -------------------------------------------------------------------

    private fun agents(c: Canvas, s: DashState, now: Instant) {
        val pad = dp(32f)
        val w = width.toFloat()
        c.drawText("SCAN ▸ ${s.agents.size} AGENT${if (s.agents.size == 1) "" else "S"}", pad, dp(48f), monoText(16f))
        if (s.agents.isEmpty()) return centered(c, "NO AGENTS ACTIVE")
        var y = dp(108f)
        for (a in s.agents.take(5)) {
            val (label, col) = when (a.state) {
                "waiting" -> "WAITING" to BLUE
                "working" -> "WORKING" to AMBER
                else -> "DONE" to GREEN
            }
            if (a.state == "waiting") hud.brackets(c, RectF(pad - dp(12f), y - dp(40f), w - pad + dp(12f), y + dp(14f)), 12f, stroke(BLUE, 2f))
            line(c, label, pad, y, dp(200f), text(30f, col, bold = true))
            line(c, a.project.ifEmpty { "session" }, pad + dp(200f), y, w - pad * 2 - dp(330f), text(28f, PRIMARY))
            val since = Logic.ago(a.since, now).removeSuffix(" ago")
            val sp = monoText(18f, SECONDARY)
            c.drawText(since, w - pad - sp.measureText(since), y, sp)
            y += dp(60f)
        }
    }

    // ---- briefing -----------------------------------------------------------------

    fun briefingShown(now: Instant) = now.isBefore(Hub.briefingUntil)

    private fun briefing(c: Canvas, s: DashState, now: Instant) {
        val pad = dp(40f)
        val w = width.toFloat()
        c.drawText("◤ SCOUTER REPORT", pad, dp(56f), monoText(22f))
        val sinceText = Hub.briefingSince?.let { "since " + Logic.ago(it, now) } ?: "last 24 h"
        val sp = monoText(16f, SECONDARY)
        c.drawText(sinceText, w - pad - sp.measureText(sinceText), dp(56f), sp)
        val events = Logic.briefing(s, Hub.briefingSince).takeLast(6)
        if (events.isEmpty()) {
            line(c, "ALL QUIET.", pad, dp(150f), w - pad * 2, text(44f, GREEN, bold = true))
            line(c, "Nothing broke while you were away. Average power ${Logic.powerText(Logic.averagePower(s.projects))}.", pad, dp(196f), w - pad * 2, text(22f, SECONDARY))
        } else {
            var y = dp(112f)
            for (e in events.reversed()) {
                val (glyph, col) = when (e.kind) {
                    "ci_failed" -> "✕ BROKE" to RED
                    "ci_recovered" -> "✓ FIXED" to GREEN
                    "deploy_ok" -> "▲ DEPLOYED" to GREEN
                    "deploy_failed" -> "✕ DEPLOY" to RED
                    else -> "·" to SECONDARY
                }
                val name = s.projects.firstOrNull { it.fullName == e.project }?.name ?: e.project.substringAfter('/')
                line(c, glyph, pad, y, dp(170f), monoText(18f, col))
                line(c, name, pad + dp(170f), y, dp(190f), text(22f, PRIMARY, bold = true))
                line(c, e.text, pad + dp(370f), y, w - pad * 2 - dp(370f), text(20f, SECONDARY))
                y += dp(42f)
            }
        }
        c.drawText("tap to close", pad, height - dp(32f), monoText(16f, SECONDARY))
    }

    // ---- grid -------------------------------------------------------------------

    private fun grid(c: Canvas, s: DashState, now: Instant) {
        val items = Logic.grid(s)
        if (items.isEmpty()) return centered(c, "NO TARGETS")
        val w = width.toFloat()
        val pad = dp(28f)

        val headY = dp(40f)
        c.drawText("SCAN ▸ ${items.size} TARGETS", pad, headY, monoText(16f))
        val issue = connectionIssue(s, now)
        val right = issue?.first ?: if (Logic.allFlawless(items)) "IT'S OVER 9000!" else "AVG PWR " + Logic.powerText(Logic.averagePower(items))
        val rp = monoText(if (issue == null && Logic.allFlawless(items)) 20f else 16f, issue?.second ?: if (Logic.allFlawless(items)) GREEN else Hud.CHROME_TEXT)
        c.drawText(right, w - pad - rp.measureText(right), headY, rp)

        val gap = dp(14f)
        val cols = Logic.GRID_COLS
        val top = gridTop()
        val bottom = height - dp(22f)
        val tw = (w - pad * 2 - gap * (cols - 1)) / cols
        val th = gridRowPitch() - gap
        val name = text(23f, PRIMARY, bold = true)
        val meta = monoText(17f, SECONDARY)
        gridScroll = gridScroll.coerceIn(0f, Logic.gridMaxScroll(items.size, gridRowPitch()))
        // Scrolled rows slide under the header strip, never over it.
        c.save()
        c.clipRect(0f, top - dp(6f), w, bottom + dp(6f))
        items.forEachIndexed { i, p ->
            val l = pad + (i % cols) * (tw + gap)
            val t = top + (i / cols) * (th + gap) - gridScroll
            if (t + th < top - dp(6f) || t > bottom + dp(6f)) return@forEachIndexed
            val r = RectF(l, t, l + tw, t + th)
            tiles += r to p
            hud.brackets(c, r, 12f, hud.chromeDim)
            val st = Logic.status(p)
            fill.color = color(st)
            c.drawRect(l + dp(8f), t + th * 0.2f, l + dp(11f), t + th * 0.8f, fill)

            val x = l + dp(22f)
            val tw2 = tw - dp(32f)
            p.deploy?.let { d ->
                val glyph = when (d.status) {
                    Status.SUCCESS -> if (p.mismatch) "▲!" else "▲"
                    Status.FAILURE -> "✕"
                    Status.RUNNING -> "◌"
                    else -> "·"
                }
                val gp = monoText(16f, if (p.mismatch) RED else color(d.status))
                c.drawText(glyph, l + tw - dp(14f) - gp.measureText(glyph), t + th * 0.30f, gp)
            }
            val nameIcon = icon(c, p, x, t + th * 0.36f, 22f, name)
            line(c, p.name, x + nameIcon, t + th * 0.36f, tw2 - dp(26f) - nameIcon, name)
            line(c, Logic.label(st), x, t + th * 0.64f, tw2, text(19f, color(st), bold = true))
            val run = p.ci
            // Compact age ("14m"): the mono font is wide and tiles are narrow.
            val age = if (run != null) Logic.runTime(run, now).takeIf { st == Status.RUNNING } ?: Logic.ago(run.startedAt, now).removeSuffix(" ago") else Logic.ago(p.pushedAt, now).removeSuffix(" ago")
            line(c, "PWR ${Logic.powerText(shownPower(p))} · $age", x, t + th * 0.88f, tw2, meta)
        }
        c.restore()

        // Off-screen tiles, counted in whole rows: snapped scrolls always land on one.
        val firstRow = Math.round(gridScroll / gridRowPitch())
        val below = (items.size - (firstRow + Logic.GRID_ROWS) * cols).coerceAtLeast(0)
        val hint = monoText(15f)
        if (below > 0) {
            val more = "▼ $below MORE"
            c.drawText(more, (w - hint.measureText(more)) / 2, height - dp(6f), hint)
        }
        if (firstRow > 0) c.drawText("▲", (w - hint.measureText("▲")) / 2, top - dp(2f), hint)
    }

    // Logos in full colour but dimmed: on black, alpha is brightness, and the
    // status colours stay the brightest thing on the screen.
    private val iconPaint = Paint(Paint.FILTER_BITMAP_FLAG or Paint.ANTI_ALIAS_FLAG).apply { alpha = 180 }
    private val iconDst = RectF()

    /**
     * Draws [p]'s icon at [x], centred on the text line at [baseline]; returns
     * the width it took, 0 when the project has no icon (yet).
     */
    private fun icon(c: Canvas, p: Project, x: Float, baseline: Float, sizeDp: Float, beside: Paint): Float {
        val bmp = Icons.get(p.icon) ?: return 0f
        val size = dp(sizeDp)
        val mid = baseline + (beside.ascent() + beside.descent()) / 2
        iconDst.set(x, mid - size / 2, x + size, mid + size / 2)
        c.drawBitmap(bmp, null, iconDst, iconPaint)
        return size + dp(8f)
    }

    /** Grid scroll offset in pixels; the activity drives it, [grid] clamps it. */
    var gridScroll = 0f

    private fun gridTop() = dp(56f)

    /** Height of one Grid row plus its gap: three of them fill the screen. */
    fun gridRowPitch(): Float {
        val gap = dp(14f)
        val rows = Logic.GRID_ROWS
        return (height - gridTop() - dp(22f) - gap * (rows - 1)) / rows + gap
    }

    fun gridMaxScroll(): Float = Logic.gridMaxScroll(Hub.state?.let { Logic.grid(it).size } ?: 0, gridRowPitch())

    // ---- alerts -----------------------------------------------------------------

    private fun freshAlert(s: DashState, now: Instant): Alert? = Logic.openAlerts(s, dismissed).firstOrNull { a ->
        val seen = Hub.alertSeenAt[a.id] ?: return@firstOrNull false
        Duration.between(seen, now).seconds < CARD_SECONDS
    }

    /** The scouter can't take it: cracked lens, power dropping. */
    private fun alertCard(c: Canvas, a: Alert, now: Instant) {
        c.drawColor(ALERT_BG)
        val w = width.toFloat()
        val h = height.toFloat()
        hud.cracks(c, w, h, stroke(0x55FFFFFF, 1.2f))
        val pad = dp(44f)
        val tw = w - pad * 2
        val project = Hub.state?.projects?.firstOrNull { it.fullName == a.project }
        val pink = 0xFFFFC2C2.toInt()
        c.drawText(Logic.alertHeadline(a.kind), pad, pad + dp(26f), monoText(20f, pink))
        line(c, project?.name ?: a.project, pad, pad + dp(96f), tw, text(50f, 0xFFFFFFFF.toInt(), bold = true))
        val current = project?.power
        val was = project?.let { previousPower[it.fullName] }
        val pwr = when {
            current == null -> "PWR ----"
            was != null && was > current -> "PWR $was → $current"
            else -> "PWR $current ▼"
        }
        c.drawText(pwr, pad, pad + dp(136f), monoText(24f, 0xFFFFFFFF.toInt()))
        val y = multiline(c, a.text, pad, pad + dp(154f), tw * 0.8f, 3, text(24f, 0xFFFFE5E5.toInt()))
        c.drawText(Logic.ago(a.at, now) + " · tap to dismiss", pad, y + dp(40f), monoText(18f, pink))
    }

    /** After the card's minute is up, an undismissed alert lives on as a thin strip. */
    private fun banner(c: Canvas, s: DashState, now: Instant) {
        val open = Logic.openAlerts(s, dismissed)
        if (open.isEmpty() || freshAlert(s, now) != null) return
        val h = bannerHeight()
        fill.color = ALERT_BG
        c.drawRect(0f, 0f, width.toFloat(), h, fill)
        val first = open.first()
        val name = s.projects.firstOrNull { it.fullName == first.project }?.name ?: first.project
        val more = if (open.size > 1) "  +${open.size - 1}" else ""
        line(c, "⚠ $name: ${first.text}$more", dp(16f), h - dp(12f), width - dp(32f), text(18f, 0xFFFFFFFF.toInt(), bold = true))
    }

    fun bannerHeight() = dp(40f)

    /** The HUD's top strip: long-pressing it opens settings. */
    fun headerHeight() = dp(70f)

    /** Connection trouble, if any, with its colour: shown in place of a HUD status label. */
    private fun connectionIssue(s: DashState, now: Instant): Pair<String, Int>? = when {
        !Hub.connected -> ("NO SIGNAL " + Logic.ago(Hub.disconnectedSince, now).removeSuffix(" ago")) to PURPLE
        Hub.polling -> "POLLING · NO STREAM" to AMBER
        s.sources["github"]?.ok == false -> "GITHUB ERROR" to AMBER
        else -> null
    }

    // ---- text helpers -----------------------------------------------------------

    /** Ellipsized strings by (text, width, paint): measuring text is the costliest part of a frame. */
    private val fitted = object : LinkedHashMap<String, String>(64, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, String>) = size > 128
    }

    private fun line(c: Canvas, s: String, x: Float, baseline: Float, maxW: Float, p: TextPaint) {
        if (maxW <= 0) return
        val key = "${maxW.toInt()}|${p.textSize}|${p.typeface.hashCode()}|$s"
        val text = fitted.getOrPut(key) { TextUtils.ellipsize(s, p, maxW, TextUtils.TruncateAt.END).toString() }
        c.drawText(text, x, baseline, p)
    }

    /** Draws up to [maxLines] lines from [top]; returns the bottom y. */
    private fun multiline(c: Canvas, s: String, x: Float, top: Float, w: Float, maxLines: Int, p: TextPaint): Float {
        val width = w.toInt().coerceAtLeast(1)
        val layout = layouts.getOrPut("$width|$maxLines|${p.textSize}|${p.color}|$s") {
            StaticLayout.Builder.obtain(s, 0, s.length, p, width)
                .setAlignment(Layout.Alignment.ALIGN_NORMAL).setMaxLines(maxLines).setEllipsize(TextUtils.TruncateAt.END).build()
        }
        c.save()
        c.translate(x, top)
        layout.draw(c)
        c.restore()
        return top + layout.height
    }

    private fun centered(c: Canvas, s: String) {
        val p = monoText(22f)
        c.drawText(s, (width - p.measureText(s)) / 2, height / 2f, p)
    }

    companion object {
        const val CARD_SECONDS = 60L
        private const val BG = 0xFF000000.toInt()
        private const val PRIMARY = 0xFFE8E8E8.toInt()
        private const val SECONDARY = 0xFF8C8C8C.toInt()
        private const val GREEN = 0xFF30D158.toInt()
        private const val RED = 0xFFFF453A.toInt()
        private const val AMBER = 0xFFFFB340.toInt()
        private const val GRAY = 0xFF6E6E73.toInt()
        private const val PURPLE = 0xFFBF5AF2.toInt()
        private const val BLUE = 0xFF4DA3FF.toInt()
        private const val ALERT_BG = 0xFF7A0909.toInt()

        fun color(s: Status) = when (s) {
            Status.SUCCESS -> GREEN
            Status.FAILURE -> RED
            Status.RUNNING -> AMBER
            Status.CANCELLED, Status.NONE -> GRAY
        }
    }
}
