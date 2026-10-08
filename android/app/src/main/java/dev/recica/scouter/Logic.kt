package dev.recica.scouter

import java.time.DayOfWeek
import java.time.Duration
import java.time.Instant
import java.time.LocalDateTime
import java.time.LocalTime

/** Every decision the UI makes, kept free of Android so it is unit-testable. */
object Logic {
    /** The Grid is 3 wide and shows 3 rows at a time; more rows scroll. */
    const val GRID_COLS = 3
    const val GRID_ROWS = 3
    const val MAX_POWER = 9000

    fun powerText(power: Int?): String = power?.toString() ?: "----"

    /** Average power of the projects that have one; null when none do. */
    fun averagePower(ps: List<Project>): Int? = ps.mapNotNull { it.power }.takeIf { it.isNotEmpty() }?.average()?.toInt()

    /** Every judged project flawless: the Grid gets to say it. */
    fun allFlawless(ps: List<Project>): Boolean = ps.any { it.power != null } && ps.all { it.power == null || it.power == MAX_POWER }

    /** The stage a project is fought on: fixed per project, spread over all stages. */
    fun stageFor(fullName: String): Stage = Stage.entries[Math.floorMod(fullName.hashCode(), Stage.entries.size)]

    /** Every Grid project passing, and at least one to pass. */
    private fun allGreen(s: DashState): Boolean = grid(s).let { g -> g.isNotEmpty() && g.all { status(it) == Status.SUCCESS } }

    /** The moment everything turns green summons the dragon; staying green does not. */
    fun wishStarts(old: DashState?, new: DashState): Boolean = old != null && !allGreen(old) && allGreen(new)

    /** Projects whose power changed between two documents: fullName to (old, new). */
    fun powerChanges(old: DashState?, new: DashState): Map<String, Pair<Int, Int>> {
        val before = old?.projects?.associate { it.fullName to it.power }.orEmpty()
        return new.projects.mapNotNull { p ->
            val was = before[p.fullName]
            if (p.power != null && was != null && was != p.power) p.fullName to (was to p.power) else null
        }.toMap()
    }

    enum class Led { GREEN, AMBER, RED, BLUE, PURPLE }

    fun waiting(s: DashState): List<Agent> = s.agents.filter { it.state == "waiting" }

    enum class Next { RECONNECT_NOW, BACK_OFF, POLL }

    /**
     * What to do after a stream attempt. A stream that connected but never
     * delivered a state is being buffered by a proxy: retrying would loop
     * forever, so poll instead. A stream that worked and then dropped is
     * routine (deploys, network blips): reconnect at once.
     */
    fun afterStream(o: StreamOutcome): Next = when (o) {
        StreamOutcome.STARVED -> Next.POLL
        StreamOutcome.ENDED_AFTER_DATA -> Next.RECONNECT_NOW
        StreamOutcome.FAILED -> Next.BACK_OFF
    }

    fun status(p: Project): Status = p.ci?.status ?: Status.NONE

    /**
     * The project Focus shows, per the server's focus mode:
     * latest = the aggregator's pick (newest activity); pinned = that repo;
     * rotate = cycles the favorites every rotateMinutes. A project tapped to
     * ([manual]) wins over latest and rotate until the next push. Falls back
     * to latest whenever the chosen project is not in the document.
     */
    fun focus(s: DashState, now: Instant = Instant.now(), manual: ManualFocus? = null): Project? {
        val byName = s.projects.associateBy { it.fullName }
        manualProject(s, manual)?.let { return it }
        val chosen = when (s.settings.focusMode) {
            "pinned" -> byName[s.settings.pinned]
            "rotate" -> s.settings.favorites.mapNotNull { byName[it] }.takeIf { it.isNotEmpty() }?.let { favs ->
                val slot = now.epochSecond / 60 / s.settings.rotateMinutes.coerceAtLeast(1)
                favs[(slot % favs.size).toInt()]
            }
            else -> null
        }
        return chosen ?: byName[s.focus] ?: s.projects.firstOrNull()
    }

    /** A project tapped to in Focus, and the newest push when it was tapped. */
    data class ManualFocus(val fullName: String, val asOf: Instant?)

    /** The newest push across all projects: a later one hands Focus back to its mode. */
    fun newestPush(s: DashState): Instant? = s.projects.mapNotNull { it.pushedAt }.maxOrNull()

    /** The tapped project while it still applies: not LOCKED, still there, no push since. */
    fun manualProject(s: DashState, manual: ManualFocus?): Project? {
        if (manual == null || locked(s)) return null
        val newest = newestPush(s)
        if (newest != null && (manual.asOf == null || newest.isAfter(manual.asOf))) return null
        return s.projects.firstOrNull { it.fullName == manual.fullName }
    }

    /** The project after [current] in Grid order, wrapping; the first one without a current. */
    fun next(s: DashState, current: Project?): Project? {
        val order = grid(s)
        if (order.isEmpty()) return null
        val i = order.indexOfFirst { it.fullName == current?.fullName }
        return order[(i + 1) % order.size]
    }

    /** Whether Focus is held on one project (shown as LOCKED). */
    fun locked(s: DashState): Boolean = s.settings.focusMode == "pinned" && s.projects.any { it.fullName == s.settings.pinned }

    /**
     * Grid order: favorites first in their chosen order, then broken, then
     * running, then everything else; each group by recent activity (the
     * document is already sorted that way).
     */
    fun grid(s: DashState): List<Project> {
        val byName = s.projects.associateBy { it.fullName }
        val favs = s.settings.favorites.mapNotNull { byName[it] }
        val rest = s.projects.filter { it.fullName !in s.settings.favorites }
            .withIndex().sortedWith(compareBy({ rank(status(it.value)) }, { it.index })).map { it.value }
        return favs + rest
    }

    /** How far the Grid can scroll: every row past the first [GRID_ROWS]. */
    fun gridMaxScroll(count: Int, rowPitch: Float): Float =
        maxOf(0, (count + GRID_COLS - 1) / GRID_COLS - GRID_ROWS) * rowPitch

    /** Where a released scroll comes to rest: the nearest whole row, within bounds. */
    fun gridSnap(offset: Float, rowPitch: Float, max: Float): Float =
        (Math.round(offset / rowPitch) * rowPitch).coerceIn(0f, max)

    /** Settings after a long-press on the Focus project: pin it, or release a pin. */
    fun togglePin(s: Settings, current: String): Settings =
        if (s.focusMode == "pinned" && s.pinned == current) s.copy(focusMode = "latest", pinned = "")
        else s.copy(focusMode = "pinned", pinned = current)

    private fun rank(st: Status) = when (st) {
        Status.FAILURE -> 0
        Status.RUNNING -> 1
        else -> 2
    }

    fun openAlerts(s: DashState, dismissed: Set<String>): List<Alert> = s.alerts.filter { it.id !in dismissed }

    /** Whether an undismissed alert could be on screen (no lens sweep over the alert card). */
    fun freshAlertVisible(s: DashState, dismissed: Set<String>): Boolean = openAlerts(s, dismissed).isNotEmpty()

    /**
     * The LED only goes red for fresh trouble (an undismissed alert): with
     * several long-red repos, "any failure" would make it permanently red and
     * therefore meaningless.
     */
    fun led(s: DashState?, connected: Boolean, dismissed: Set<String>): Led = when {
        s == null || !connected -> Led.PURPLE
        openAlerts(s, dismissed).isNotEmpty() -> Led.RED
        waiting(s).isNotEmpty() -> Led.BLUE
        s.projects.any { it.ci?.status == Status.RUNNING || it.latest?.status == Status.RUNNING } -> Led.AMBER
        else -> Led.GREEN
    }

    /**
     * Wave detection on the proximity sensor: a hand passing over the phone
     * reads near and then far again within [WAVE_MS]. Keeping it here keeps
     * the rule testable; the service just feeds it sensor events.
     */
    class Wave {
        private var nearAt = -1L

        /** Feed one reading; true when it completes a wave. */
        fun onReading(near: Boolean, atMs: Long): Boolean {
            if (near) {
                nearAt = atMs
                return false
            }
            val waved = nearAt >= 0 && atMs - nearAt in 0..WAVE_MS
            nearAt = -1
            return waved
        }

        companion object {
            const val WAVE_MS = 1_500L
        }
    }

    /** Events for the morning briefing: what happened since the screen went dark. */
    fun briefing(s: DashState, since: Instant?): List<Event> =
        s.events.filter { e -> since == null || (e.at != null && e.at.isAfter(since)) }

    /** "1-5 09:00-19:00": days (1 = Monday) and the hours the screen is on. */
    data class Schedule(val firstDay: Int, val lastDay: Int, val on: LocalTime, val off: LocalTime) {
        fun isOn(t: LocalDateTime): Boolean {
            val d = t.dayOfWeek.value
            val dayOk = if (firstDay <= lastDay) d in firstDay..lastDay else d >= firstDay || d <= lastDay
            val tt = t.toLocalTime()
            return dayOk && !tt.isBefore(on) && tt.isBefore(off)
        }

        companion object {
            val DEFAULT = Schedule(DayOfWeek.MONDAY.value, DayOfWeek.FRIDAY.value, LocalTime.of(9, 0), LocalTime.of(19, 0))

            fun of(s: Settings): Schedule = parse("${s.days} ${s.on}-${s.off}")

            fun parse(spec: String?): Schedule = runCatching {
                val (days, hours) = spec!!.trim().split(" ")
                val (d1, d2) = days.split("-").map { it.toInt() }
                val (h1, h2) = hours.split("-").map { LocalTime.parse(it) }
                Schedule(d1, d2, h1, h2)
            }.getOrDefault(DEFAULT)
        }
    }

    fun ago(t: Instant?, now: Instant): String {
        if (t == null) return ""
        val s = Duration.between(t, now).seconds.coerceAtLeast(0)
        return when {
            s < 60 -> "just now"
            s < 3600 -> "${s / 60}m ago"
            s < 86400 -> "${s / 3600}h ago"
            else -> "${s / 86400}d ago"
        }
    }

    fun duration(seconds: Long): String = when {
        seconds < 60 -> "${seconds}s"
        seconds < 3600 -> "${seconds / 60}m ${seconds % 60}s"
        else -> "${seconds / 3600}h ${seconds % 3600 / 60}m"
    }

    /** Elapsed time for a running build, final duration for a finished one. */
    fun runTime(r: Run, now: Instant): String = when {
        r.status == Status.RUNNING && r.startedAt != null -> duration(Duration.between(r.startedAt, now).seconds.coerceAtLeast(0))
        r.durationS > 0 -> duration(r.durationS.toLong())
        else -> ""
    }

    fun deployLabel(st: Status) = when (st) {
        Status.SUCCESS -> "LIVE"
        Status.FAILURE -> "DEPLOY FAILED"
        Status.RUNNING -> "DEPLOYING"
        Status.CANCELLED -> "CANCELLED"
        Status.NONE -> "UNKNOWN"
    }

    /** "running:healthy" -> "healthy"; empty when Coolify reports nothing useful. */
    fun health(raw: String): String = raw.substringAfter(':', raw).takeIf { it.isNotBlank() && it != "unknown" }.orEmpty()

    /** Header of the alert card, per kind of trouble. */
    fun alertHeadline(kind: String) = when (kind) {
        "deploy_failed" -> "⚠ DEPLOY FAILED"
        "deployed_red" -> "⚠ DEPLOYED WHILE CI RED"
        else -> "⚠ POWER LEVEL DROPPING"
    }

    fun label(st: Status) = when (st) {
        Status.RUNNING -> "RUNNING"
        Status.SUCCESS -> "PASSING"
        Status.FAILURE -> "FAILED"
        Status.CANCELLED -> "CANCELLED"
        Status.NONE -> "NO CI"
    }
}
