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

    private fun p(name: String, st: Status?, latest: Status? = null, power: Int? = null) = Project(
        name, "me/$name", "master", now,
        st?.let { Run(it, "master", "msg", "CI", now.minusSeconds(90), 0) },
        latest?.let { Run(it, "feat", "msg", "CI", now, 0) }, power, 0,
    )

    private fun state(vararg ps: Project, alerts: List<Alert> = emptyList()) =
        DashState(1, ps.firstOrNull()?.fullName ?: "", ps.toList(), alerts, emptyMap())

    @Test
    fun parsesAggregatorDocument() {
        val s = Parser.parse(
            """{"version":7,"focus":"me/a","projects":[{"name":"a","full_name":"me/a","default_branch":"master",
               "pushed_at":"2026-10-07T11:00:00Z","ci":{"status":"failure","branch":"master","sha":"x","title":"fix it",
               "workflow":"Lint","started_at":"2026-10-07T11:01:00Z","duration_s":75,"url":"u"},"power":6750,"open_prs":2,
               "deploy":{"status":"success","commit":"x1234567","branch":"master","at":"2026-10-07T11:05:00Z","health":"running:healthy","apps":2},"mismatch":true,"icon":"0123456789abcdef"},
               {"name":"b","full_name":"me/b","default_branch":"main","open_prs":0},
               {"name":"c","full_name":"me/c","default_branch":"main","open_prs":0,"icon":"../../etc/passwd"}],
               "alerts":[{"id":"ci:me/a:x","kind":"ci_failed","project":"me/a","text":"t","at":"2026-10-07T13:03:00.123456789+02:00"}],
               "sources":{"github":{"ok":false,"error":"boom","updated_at":"2026-10-07T11:00:00Z"}}}""",
        )
        assertEquals(7, s.version)
        val a = s.projects.first()
        assertEquals(6750, a.power)
        assertEquals(Status.SUCCESS, a.deploy!!.status)
        assertEquals(2, a.deploy!!.apps)
        assertTrue(a.mismatch)
        assertNull(s.projects[1].deploy)
        assertEquals("0123456789abcdef", a.icon)
        assertNull(s.projects[1].icon) // none found
        assertNull(s.projects[2].icon) // never trust anything but a hash in a file name
        assertEquals("healthy", Logic.health(a.deploy!!.health))
        assertEquals("", Logic.health("unknown"))
        assertEquals("⚠ DEPLOYED WHILE CI RED", Logic.alertHeadline("deployed_red"))
        assertNull(s.projects[1].power)
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
    fun focusFollowsServerFocusMode() {
        val base = state(p("a", Status.SUCCESS), p("b", Status.SUCCESS), p("c", Status.SUCCESS))
        assertEquals("me/a", Logic.focus(base, now)!!.fullName) // latest = the aggregator's pick

        val pinned = base.copy(settings = Settings(focusMode = "pinned", pinned = "me/b"))
        assertEquals("me/b", Logic.focus(pinned, now)!!.fullName)
        assertTrue(Logic.locked(pinned))
        val gone = base.copy(settings = Settings(focusMode = "pinned", pinned = "me/gone"))
        assertEquals("me/a", Logic.focus(gone, now)!!.fullName)
        assertFalse(Logic.locked(gone))

        val rotate = base.copy(settings = Settings(focusMode = "rotate", rotateMinutes = 5, favorites = listOf("me/c", "me/missing", "me/b")))
        val t0 = Instant.parse("2026-10-07T12:00:00Z") // minute 29_342_160, divisible by 5: slot even
        val first = Logic.focus(rotate, t0)!!.fullName
        val next = Logic.focus(rotate, t0.plusSeconds(5 * 60))!!.fullName
        assertEquals(setOf("me/b", "me/c"), setOf(first, next)) // cycles present favorites only
        assertEquals(first, Logic.focus(rotate, t0.plusSeconds(4 * 60))!!.fullName) // stable within a slot
    }

    @Test
    fun togglePin() {
        val pinned = Logic.togglePin(Settings(), "me/a")
        assertEquals("pinned" to "me/a", pinned.focusMode to pinned.pinned)
        val released = Logic.togglePin(pinned, "me/a")
        assertEquals("latest" to "", released.focusMode to released.pinned)
        assertEquals("me/b", Logic.togglePin(pinned, "me/b").pinned)
    }

    @Test
    fun gridShowsFavoritesFirstInTheirOrder() {
        val s = state(p("a", Status.SUCCESS), p("broken", Status.FAILURE), p("c", Status.SUCCESS), p("d", Status.RUNNING))
            .copy(settings = Settings(favorites = listOf("me/c", "me/a")))
        assertEquals(listOf("c", "a", "broken", "d"), Logic.grid(s).map { it.name })
    }

    @Test
    fun settingsRoundTrip() {
        val s = Settings(hidden = listOf("me/x"), favorites = listOf("me/a", "me/b"), focusMode = "rotate", rotateMinutes = 7,
            days = "6-2", on = "08:30", off = "20:15", kiosk = true, aura = false, stars = true, mesh = true, stage = false, weather = false, alertHours = 3,
            wave = false, briefing = false)
        assertEquals(s, Settings.parse(org.json.JSONObject(s.toJson())))
        assertEquals(Settings(), Settings.parse(null)) // older server: defaults
    }

    @Test
    fun gridPutsBrokenThenRunningFirstAndKeepsAll() {
        val ps = (1..12).map { p("p$it", Status.SUCCESS) }.toMutableList()
        ps[5] = p("broken", Status.FAILURE)
        ps[3] = p("busy", Status.RUNNING)
        val g = Logic.grid(state(*ps.toTypedArray()))
        assertEquals(12, g.size) // more than fit on screen: the Grid scrolls
        assertEquals(listOf("broken", "busy", "p1", "p2", "p3"), g.take(5).map { it.name })
    }

    @Test
    fun eachProjectKeepsItsStageAndAllStagesGetUsed() {
        assertEquals(Logic.stageFor("me/igris"), Logic.stageFor("me/igris"))
        val used = (1..40).map { Logic.stageFor("me/project-$it") }.toSet()
        assertEquals(Stage.entries.toSet(), used)
    }

    @Test
    fun theWishStartsOnlyWhenEverythingTurnsGreen() {
        val green = state(p("a", Status.SUCCESS), p("b", Status.SUCCESS))
        val broken = state(p("a", Status.SUCCESS), p("b", Status.FAILURE))
        val busy = state(p("a", Status.SUCCESS), p("b", Status.RUNNING))
        assertTrue(Logic.wishStarts(broken, green))
        assertTrue(Logic.wishStarts(busy, green))
        assertFalse(Logic.wishStarts(green, green)) // already green: no repeat on every update
        assertFalse(Logic.wishStarts(null, green)) // not on app start
        assertFalse(Logic.wishStarts(green, broken))
        assertFalse(Logic.wishStarts(broken, state())) // nothing to be green
    }

    @Test
    fun gridScrollsOnlyPastThreeRows() {
        val pitch = 100f
        assertEquals(0f, Logic.gridMaxScroll(0, pitch))
        assertEquals(0f, Logic.gridMaxScroll(9, pitch))
        assertEquals(100f, Logic.gridMaxScroll(10, pitch))
        assertEquals(100f, Logic.gridMaxScroll(12, pitch))
        assertEquals(200f, Logic.gridMaxScroll(15, pitch))
    }

    @Test
    fun gridSnapsToWholeRowsWithinBounds() {
        assertEquals(0f, Logic.gridSnap(40f, 100f, 200f))
        assertEquals(100f, Logic.gridSnap(60f, 100f, 200f))
        assertEquals(200f, Logic.gridSnap(260f, 100f, 200f)) // past the end
        assertEquals(0f, Logic.gridSnap(-80f, 100f, 200f)) // above the top
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

    @Test
    fun powerHelpers() {
        val flawless = listOf(p("a", Status.SUCCESS, power = 9000), p("b", null))
        assertTrue(Logic.allFlawless(flawless))
        assertFalse(Logic.allFlawless(flawless + p("c", Status.FAILURE, power = 6750)))
        assertFalse(Logic.allFlawless(listOf(p("x", null)))) // nothing judged: no bragging
        assertEquals(7875, Logic.averagePower(listOf(p("a", null, power = 9000), p("b", null, power = 6750), p("c", null))))
        assertNull(Logic.averagePower(listOf(p("c", null))))
        assertEquals("----", Logic.powerText(null))

        val old = state(p("a", null, power = 9000), p("b", null, power = 4500), p("c", null))
        val new = state(p("a", null, power = 6750), p("b", null, power = 4500), p("c", null, power = 9000))
        assertEquals(mapOf("me/a" to (9000 to 6750)), Logic.powerChanges(old, new))
        assertTrue(Logic.powerChanges(null, new).isEmpty())
    }

    @Test
    fun streamFallback() {
        assertEquals(Logic.Next.POLL, Logic.afterStream(StreamOutcome.STARVED))
        assertEquals(Logic.Next.RECONNECT_NOW, Logic.afterStream(StreamOutcome.ENDED_AFTER_DATA))
        assertEquals(Logic.Next.BACK_OFF, Logic.afterStream(StreamOutcome.FAILED))
    }

    @Test
    fun ledBlueWhenAnAgentWaits() {
        val waiting = state(p("a", Status.SUCCESS)).copy(agents = listOf(Agent("s", "igris", "waiting", now)))
        assertEquals(Logic.Led.BLUE, Logic.led(waiting, true, emptySet()))
        val alert = Alert("x", "ci_failed", "me/a", "t", now)
        assertEquals(Logic.Led.RED, Logic.led(waiting.copy(alerts = listOf(alert)), true, emptySet())) // red still wins
    }

    @Test
    fun wave() {
        val w = Logic.Wave()
        assertFalse(w.onReading(false, 0))
        assertFalse(w.onReading(true, 1_000))
        assertTrue(w.onReading(false, 1_600)) // near for 0.6 s: a wave
        assertFalse(w.onReading(true, 5_000))
        assertFalse(w.onReading(false, 9_000)) // near for 4 s: something parked on it, not a wave
        assertFalse(w.onReading(false, 9_100)) // far without near first
    }

    @Test
    fun parsesHistoryAgentsEventsAndBriefing() {
        val s = Parser.parse(
            """{"version":1,"focus":"","projects":[],"alerts":[],"sources":{},
               "history":{"me/a":[-1,9000,6750]},
               "agents":[{"id":"s1","project":"igris","state":"waiting","since":"2026-10-07T11:00:00Z"}],
               "events":[{"at":"2026-10-07T02:00:00Z","project":"me/a","kind":"ci_failed","text":"x"},
                         {"at":"2026-10-07T10:00:00Z","project":"me/a","kind":"ci_recovered","text":"y"}]}""",
        )
        assertEquals(listOf(-1, 9000, 6750), s.history["me/a"])
        assertEquals("igris", s.agents.single().project)
        assertEquals(listOf("ci_recovered"), Logic.briefing(s, Instant.parse("2026-10-07T05:00:00Z")).map { it.kind })
        assertEquals(2, Logic.briefing(s, null).size)
    }
}
