package dev.recica.scouter

import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant
import java.time.OffsetDateTime

/** Mirrors the aggregator's state document (aggregator/internal/state/state.go). */
data class DashState(
    val version: Long,
    val focus: String,
    val projects: List<Project>,
    val alerts: List<Alert>,
    val sources: Map<String, Source>,
    val settings: Settings = Settings(),
    /** Every trackable repo, for the settings pickers. */
    val available: List<String> = emptyList(),
    /** False for an aggregator too old to send settings: the phone then uses its local fallbacks. */
    val hasSettings: Boolean = false,
    /** The APK the server wants this phone to run (over-the-air update). */
    val app: AppRelease? = null,
    /** Power over the last 14 days per project, oldest first, -1 = unknown. */
    val history: Map<String, List<Int>> = emptyMap(),
    /** Live Claude Code sessions, waiting ones first. */
    val agents: List<Agent> = emptyList(),
    /** What happened in the last 24 h, oldest first (morning briefing). */
    val events: List<Event> = emptyList(),
)

data class Agent(val id: String, val project: String, val state: String, val since: Instant?)

data class Event(val at: Instant?, val project: String, val kind: String, val text: String)

data class AppRelease(val sha256: String, val size: Long)

/**
 * Mirrors aggregator/internal/state/settings.go. The server owns it; the
 * phone edits by sending the whole object back (PUT /v1/settings), so every
 * field must round-trip through [toJson].
 */
data class Settings(
    val hidden: List<String> = emptyList(),
    val favorites: List<String> = emptyList(),
    val focusMode: String = "latest",
    val pinned: String = "",
    val rotateMinutes: Int = 5,
    val days: String = "1-5",
    val on: String = "09:00",
    val off: String = "19:00",
    val kiosk: Boolean = false,
    val aura: Boolean = true,
    val stars: Boolean = true,
    val mesh: Boolean = false,
    val alertHours: Int = 12,
    val wave: Boolean = true,
    val briefing: Boolean = true,
) {
    fun toJson(): String = JSONObject()
        .put("hidden", JSONArray(hidden))
        .put("favorites", JSONArray(favorites))
        .put("focus_mode", focusMode)
        .put("pinned", pinned)
        .put("rotate_minutes", rotateMinutes)
        .put("schedule", JSONObject().put("days", days).put("on", on).put("off", off))
        .put("kiosk", kiosk)
        .put("background", JSONObject().put("aura", aura).put("stars", stars).put("mesh", mesh))
        .put("alert_hours", alertHours)
        .put("wave", wave)
        .put("briefing", briefing)
        .toString()

    companion object {
        fun parse(o: JSONObject?): Settings {
            if (o == null) return Settings()
            val sch = o.optJSONObject("schedule")
            val bg = o.optJSONObject("background")
            val d = Settings()
            return Settings(
                hidden = strings(o.optJSONArray("hidden")),
                favorites = strings(o.optJSONArray("favorites")),
                focusMode = o.optString("focus_mode", d.focusMode),
                pinned = o.optString("pinned"),
                rotateMinutes = o.optInt("rotate_minutes", d.rotateMinutes),
                days = sch?.optString("days", d.days) ?: d.days,
                on = sch?.optString("on", d.on) ?: d.on,
                off = sch?.optString("off", d.off) ?: d.off,
                kiosk = o.optBoolean("kiosk"),
                aura = bg?.optBoolean("aura", d.aura) ?: d.aura,
                stars = bg?.optBoolean("stars", d.stars) ?: d.stars,
                mesh = bg?.optBoolean("mesh", d.mesh) ?: d.mesh,
                alertHours = o.optInt("alert_hours", d.alertHours),
                wave = o.optBoolean("wave", d.wave),
                briefing = o.optBoolean("briefing", d.briefing),
            )
        }

        fun strings(a: JSONArray?): List<String> = a?.let { List(it.length()) { i -> it.getString(i) } }.orEmpty()
    }
}

data class Project(
    val name: String,
    val fullName: String,
    val defaultBranch: String,
    val pushedAt: Instant?,
    val ci: Run?,
    val latest: Run?,
    /** Build health 0..9000 (share of recent default-branch commits that passed); null = no record yet. */
    val power: Int?,
    val openPRs: Int,
    /** Latest Coolify deployment, when the repo has a Coolify app. */
    val deploy: Deploy? = null,
    /** The deployed commit is one whose CI failed. */
    val mismatch: Boolean = false,
    /** Hash of the project's icon on the aggregator (see [Icons]); null = none. */
    val icon: String? = null,
)

data class Deploy(
    /** RUNNING covers queued and in-progress deploys. */
    val status: Status,
    val commit: String,
    val branch: String,
    val at: Instant?,
    /** Coolify's container state, e.g. "running:healthy". */
    val health: String,
    val apps: Int,
)

data class Run(
    val status: Status,
    val branch: String,
    val title: String,
    val workflow: String,
    val startedAt: Instant?,
    val durationS: Int,
)

enum class Status { RUNNING, SUCCESS, FAILURE, CANCELLED, NONE }

/** How one attempt at the live stream ended. */
enum class StreamOutcome { ENDED_AFTER_DATA, STARVED, FAILED }

data class Alert(val id: String, val kind: String, val project: String, val text: String, val at: Instant?)

data class Source(val ok: Boolean, val error: String)

object Parser {
    fun parse(json: String): DashState {
        val o = JSONObject(json)
        val projects = o.optJSONArray("projects")?.let { a -> List(a.length()) { project(a.getJSONObject(it)) } }.orEmpty()
        val alerts = o.optJSONArray("alerts")?.let { a ->
            List(a.length()) {
                val x = a.getJSONObject(it)
                Alert(x.getString("id"), x.optString("kind"), x.optString("project"), x.optString("text"), instant(x, "at"))
            }
        }.orEmpty()
        val sources = buildMap {
            o.optJSONObject("sources")?.let { s ->
                for (k in s.keys()) put(k, s.getJSONObject(k).let { Source(it.optBoolean("ok"), it.optString("error")) })
            }
        }
        return DashState(
            o.optLong("version"), o.optString("focus"), projects, alerts, sources,
            Settings.parse(o.optJSONObject("settings")), Settings.strings(o.optJSONArray("available")),
            hasSettings = o.has("settings"),
            history = o.optJSONObject("history")?.let { h ->
                h.keys().asSequence().associateWith { k -> h.getJSONArray(k).let { a -> List(a.length()) { a.getInt(it) } } }
            }.orEmpty(),
            agents = o.optJSONArray("agents")?.let { a ->
                List(a.length()) { a.getJSONObject(it).let { x -> Agent(x.optString("id"), x.optString("project"), x.optString("state"), instant(x, "since")) } }
            }.orEmpty(),
            events = o.optJSONArray("events")?.let { a ->
                List(a.length()) { a.getJSONObject(it).let { x -> Event(instant(x, "at"), x.optString("project"), x.optString("kind"), x.optString("text")) } }
            }.orEmpty(),
            app = o.optJSONObject("app")?.let { AppRelease(it.optString("sha256"), it.optLong("size")) }?.takeIf { it.sha256.length == 64 },
        )
    }

    private val ICON_HASH = Regex("[0-9a-f]{16}")

    private fun project(o: JSONObject) = Project(
        name = o.getString("name"),
        fullName = o.getString("full_name"),
        defaultBranch = o.optString("default_branch"),
        pushedAt = instant(o, "pushed_at"),
        ci = o.optJSONObject("ci")?.let(::run),
        latest = o.optJSONObject("latest")?.let(::run),
        power = if (o.has("power")) o.getInt("power") else null,
        openPRs = o.optInt("open_prs"),
        icon = o.optString("icon").takeIf { ICON_HASH.matches(it) },
        deploy = o.optJSONObject("deploy")?.let { d ->
            Deploy(
                status = when (d.optString("status")) {
                    "success" -> Status.SUCCESS
                    "failure" -> Status.FAILURE
                    "running", "queued" -> Status.RUNNING
                    "cancelled" -> Status.CANCELLED
                    else -> Status.NONE
                },
                commit = d.optString("commit"),
                branch = d.optString("branch"),
                at = instant(d, "at"),
                health = d.optString("health"),
                apps = d.optInt("apps", 1),
            )
        },
        mismatch = o.optBoolean("mismatch"),
    )

    private fun run(o: JSONObject) = Run(
        status = when (o.optString("status")) {
            "running" -> Status.RUNNING
            "success" -> Status.SUCCESS
            "failure" -> Status.FAILURE
            "cancelled" -> Status.CANCELLED
            else -> Status.NONE
        },
        branch = o.optString("branch"),
        title = o.optString("title"),
        workflow = o.optString("workflow"),
        startedAt = instant(o, "started_at"),
        durationS = o.optInt("duration_s"),
    )

    // OffsetDateTime, not Instant.parse: the latter rejects "+02:00" offsets before API 33.
    private fun instant(o: JSONObject, key: String): Instant? =
        o.optString(key).takeIf { it.isNotEmpty() && !it.startsWith("0001-") }?.let { runCatching { OffsetDateTime.parse(it).toInstant() }.getOrNull() }
}
