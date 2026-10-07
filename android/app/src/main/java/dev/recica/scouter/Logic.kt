package dev.recica.scouter

import java.time.DayOfWeek
import java.time.Duration
import java.time.Instant
import java.time.LocalDateTime
import java.time.LocalTime

/** Every decision the UI makes, kept free of Android so it is unit-testable. */
object Logic {
    const val GRID_TILES = 9
    const val MAX_POWER = 9000

    fun powerText(power: Int?): String = power?.toString() ?: "----"

    /** Average power of the projects that have one; null when none do. */
    fun averagePower(ps: List<Project>): Int? = ps.mapNotNull { it.power }.takeIf { it.isNotEmpty() }?.average()?.toInt()

    /** Every judged project flawless: the Grid gets to say it. */
    fun allFlawless(ps: List<Project>): Boolean = ps.any { it.power != null } && ps.all { it.power == null || it.power == MAX_POWER }

    /** Projects whose power changed between two documents: fullName to (old, new). */
    fun powerChanges(old: DashState?, new: DashState): Map<String, Pair<Int, Int>> {
        val before = old?.projects?.associate { it.fullName to it.power }.orEmpty()
        return new.projects.mapNotNull { p ->
            val was = before[p.fullName]
            if (p.power != null && was != null && was != p.power) p.fullName to (was to p.power) else null
        }.toMap()
    }

    enum class Led { GREEN, AMBER, RED, PURPLE }

    fun status(p: Project): Status = p.ci?.status ?: Status.NONE

    /**
     * The project Focus shows, per the server's focus mode:
     * latest = the aggregator's pick (newest activity); pinned = that repo;
     * rotate = cycles the favorites every rotateMinutes. Falls back to latest
     * whenever the chosen project is not in the document.
     */
    fun focus(s: DashState, now: Instant = Instant.now()): Project? {
        val byName = s.projects.associateBy { it.fullName }
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
        return (favs + rest).take(GRID_TILES)
    }

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

    /**
     * The LED only goes red for fresh trouble (an undismissed alert): with
     * several long-red repos, "any failure" would make it permanently red and
     * therefore meaningless.
     */
    fun led(s: DashState?, connected: Boolean, dismissed: Set<String>): Led = when {
        s == null || !connected -> Led.PURPLE
        openAlerts(s, dismissed).isNotEmpty() -> Led.RED
        s.projects.any { it.ci?.status == Status.RUNNING || it.latest?.status == Status.RUNNING } -> Led.AMBER
        else -> Led.GREEN
    }

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

    fun label(st: Status) = when (st) {
        Status.RUNNING -> "RUNNING"
        Status.SUCCESS -> "PASSING"
        Status.FAILURE -> "FAILED"
        Status.CANCELLED -> "CANCELLED"
        Status.NONE -> "NO CI"
    }
}
