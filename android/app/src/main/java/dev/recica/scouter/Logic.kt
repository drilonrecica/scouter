package dev.recica.scouter

import java.time.DayOfWeek
import java.time.Duration
import java.time.Instant
import java.time.LocalDateTime
import java.time.LocalTime

/** Every decision the UI makes, kept free of Android so it is unit-testable. */
object Logic {
    const val GRID_TILES = 9

    enum class Led { GREEN, AMBER, RED, PURPLE }

    fun status(p: Project): Status = p.ci?.status ?: Status.NONE

    /** Pinned project if it is still in the document, else the aggregator's auto focus. */
    fun focus(s: DashState, pinned: String?): Project? =
        s.projects.firstOrNull { it.fullName == pinned }
            ?: s.projects.firstOrNull { it.fullName == s.focus }
            ?: s.projects.firstOrNull()

    /**
     * Grid order: broken first, then running, then everything else; each group
     * by recent activity (the document is already sorted that way).
     */
    fun grid(s: DashState): List<Project> =
        s.projects.withIndex().sortedWith(compareBy({ rank(status(it.value)) }, { it.index })).map { it.value }.take(GRID_TILES)

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
