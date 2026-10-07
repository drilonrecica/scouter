package dev.recica.scouter

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant
import java.time.LocalDateTime

class LogicTest {
    private val now: Instant = Instant.parse("2026-10-07T12:00:00Z")

    private fun p(name: String, st: Status?, latest: Status? = null) = Project(
        name, "me/$name", "master", now,
        st?.let { Run(it, "master", "msg", "CI", now.minusSeconds(90), 0) },
        latest?.let { Run(it, "feat", "msg", "CI", now, 0) }, 0,
    )

    private fun state(vararg ps: Project, alerts: List<Alert> = emptyList()) =
        DashState(1, ps.firstOrNull()?.fullName ?: "", ps.toList(), alerts, emptyMap())

    @Test
    fun parsesAggregatorDocument() {
        val s = Parser.parse(
            """{"version":7,"focus":"me/a","projects":[{"name":"a","full_name":"me/a","default_branch":"master",
               "pushed_at":"2026-10-07T11:00:00Z","ci":{"status":"failure","branch":"master","sha":"x","title":"fix it",
               "workflow":"Lint","started_at":"2026-10-07T11:01:00Z","duration_s":75,"url":"u"},"open_prs":2}],
               "alerts":[{"id":"ci:me/a:x","kind":"ci_failed","project":"me/a","text":"t","at":"2026-10-07T13:03:00.123456789+02:00"}],
               "sources":{"github":{"ok":false,"error":"boom","updated_at":"2026-10-07T11:00:00Z"}}}""",
        )
        assertEquals(7, s.version)
        val a = s.projects.single()
        assertEquals(Status.FAILURE, a.ci!!.status)
        assertEquals("Lint", a.ci!!.workflow)
        assertEquals(75, a.ci!!.durationS)
        assertNull(a.latest)
        assertEquals(2, a.openPRs)
        assertEquals("ci:me/a:x", s.alerts.single().id)
        assertEquals(Instant.parse("2026-10-07T11:03:00.123456789Z"), s.alerts.single().at) // offset + nanos
        assertFalse(s.sources.getValue("github").ok)
    }

    @Test
    fun focusPrefersPinnedThenAuto() {
        val s = state(p("a", Status.SUCCESS), p("b", Status.SUCCESS))
        assertEquals("me/b", Logic.focus(s, "me/b")!!.fullName)
        assertEquals("me/a", Logic.focus(s, "me/gone")!!.fullName)
        assertEquals("me/a", Logic.focus(s, null)!!.fullName)
    }

    @Test
    fun gridPutsBrokenThenRunningFirstAndCaps() {
        val ps = (1..12).map { p("p$it", Status.SUCCESS) }.toMutableList()
        ps[5] = p("broken", Status.FAILURE)
        ps[3] = p("busy", Status.RUNNING)
        val g = Logic.grid(state(*ps.toTypedArray()))
        assertEquals(Logic.GRID_TILES, g.size)
        assertEquals(listOf("broken", "busy", "p1", "p2", "p3"), g.take(5).map { it.name })
    }

    @Test
    fun ledOnlyRedForFreshAlerts() {
        val alert = Alert("ci:me/a:x", "ci_failed", "me/a", "t", now)
        val oldRed = state(p("a", Status.FAILURE))
        assertEquals(Logic.Led.GREEN, Logic.led(oldRed, true, emptySet()))
        assertEquals(Logic.Led.RED, Logic.led(state(p("a", Status.FAILURE), alerts = listOf(alert)), true, emptySet()))
        assertEquals(Logic.Led.GREEN, Logic.led(state(p("a", Status.FAILURE), alerts = listOf(alert)), true, setOf(alert.id)))
        assertEquals(Logic.Led.AMBER, Logic.led(state(p("a", Status.SUCCESS, latest = Status.RUNNING)), true, emptySet()))
        assertEquals(Logic.Led.PURPLE, Logic.led(oldRed, false, emptySet()))
    }

    @Test
    fun schedule() {
        val s = Logic.Schedule.parse("1-5 09:00-19:00")
        val wed = LocalDateTime.of(2026, 10, 7, 9, 0) // a Wednesday
        assertTrue(s.isOn(wed))
        assertFalse(s.isOn(wed.withHour(19)))
        assertFalse(s.isOn(wed.withHour(8).withMinute(59)))
        assertFalse(s.isOn(wed.plusDays(3))) // Saturday
        assertEquals(Logic.Schedule.DEFAULT, Logic.Schedule.parse("garbage"))
        assertTrue(Logic.Schedule.parse("6-2 10:00-12:00").isOn(wed.plusDays(5).withHour(11))) // wraps Sat..Tue
    }

    @Test
    fun runTimeCountsUpWhileRunning() {
        val r = Run(Status.RUNNING, "master", "m", "CI", now.minusSeconds(133), 0)
        assertEquals("2m 13s", Logic.runTime(r, now))
        assertEquals("1m 15s", Logic.runTime(r.copy(status = Status.SUCCESS, durationS = 75), now))
        assertEquals("3h ago", Logic.ago(now.minusSeconds(3 * 3600 + 5), now))
    }
}
