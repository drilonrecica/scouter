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
    /** Document format; above [Parser.SCHEMA] this app is too old to read it all. */
    val schema: Int = Parser.SCHEMA,
    /** The server's settings object as sent, so a save keeps fields this app does not know. */
    val settingsJson: String? = null,
)

data class Agent(val id: String, val project: String, val state: String, val since: Instant?)

data class Event(val at: Instant?, val project: String, val kind: String, val text: String)

data class AppRelease(val sha256: String, val size: Long)

/**
 * Mirrors aggregator/internal/state/settings.go. The server owns it; the
 * phone edits by sending the whole object back (PUT /v1/settings), so every
 * field must round-trip through [toJson], and fields only a newer server
 * knows are carried over from its own copy ([toJson]'s base).
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
    val stage: Boolean = true,
    val weather: Boolean = true,
    val alertHours: Int = 12,
    val wave: Boolean = true,
    val briefing: Boolean = true,
) {
    /** This object as JSON, written over [base] (the server's last copy) so unknown fields survive. */
    fun toJson(base: String? = null): String {
        val o = base?.let { runCatching { JSONObject(it) }.getOrNull() } ?: JSONObject()
        fun nested(key: String) = o.optJSONObject(key) ?: JSONObject()
        return o
            .put("hidden", JSONArray(hidden))
            .put("favorites", JSONArray(favorites))
            .put("focus_mode", focusMode)
            .put("pinned", pinned)
            .put("rotate_minutes", rotateMinutes)
            .put("schedule", nested("schedule").put("days", days).put("on", on).put("off", off))
            .put("kiosk", kiosk)
            .put("background", nested("background").put("aura", aura).put("stars", stars).put("mesh", mesh).put("stage", stage).put("weather", weather))
            .put("alert_hours", alertHours)
            .put("wave", wave)
            .put("briefing", briefing)
            .toString()
    }

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
                stage = bg?.optBoolean("stage", d.stage) ?: d.stage,
                weather = bg?.optBoolean("weather", d.weather) ?: d.weather,
                alertHours = o.optInt("alert_hours", d.alertHours),
                wave = o.optBoolean("wave", d.wave),
                briefing = o.optBoolean("briefing", d.briefing),
            )
        }

        fun strings(a: JSONArray?): List<String> = a?.let { List(it.length()) { i -> it.optString(i) }.filter(String::isNotEmpty) }.orEmpty()
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
    /** The document format this app understands (aggregator: state.Schema). */
    const val SCHEMA = 1

    /**
     * Reads the state document. Only a document that is not JSON at all
     * fails: an item the app cannot read (a project without a name, say) is
     * skipped, so one odd entry never blanks the whole dashboard.
     */
    fun parse(json: String): DashState {
        val o = JSONObject(json)
        val projects = items(o.optJSONArray("projects"), ::project)
        val alerts = items(o.optJSONArray("alerts")) { x ->
            Alert(x.getString("id"), x.optString("kind"), x.optString("project"), x.optString("text"), instant(x, "at"))
        }
        val sources = buildMap {
            o.optJSONObject("sources")?.let { s ->
                for (k in s.keys()) s.optJSONObject(k)?.let { put(k, Source(it.optBoolean("ok"), it.optString("error"))) }
            }
        }
        return DashState(
            o.optLong("version"), o.optString("focus"), projects, alerts, sources,
            Settings.parse(o.optJSONObject("settings")), Settings.strings(o.optJSONArray("available")),
            hasSettings = o.has("settings"),
            history = o.optJSONObject("history")?.let { h ->
                h.keys().asSequence().mapNotNull { k -> h.optJSONArray(k)?.let { a -> k to List(a.length()) { a.optInt(it, -1) } } }.toMap()
            }.orEmpty(),
            agents = items(o.optJSONArray("agents")) { x -> Agent(x.optString("id"), x.optString("project"), x.optString("state"), instant(x, "since")) },
            events = items(o.optJSONArray("events")) { x -> Event(instant(x, "at"), x.optString("project"), x.optString("kind"), x.optString("text")) },
            app = o.optJSONObject("app")?.let { AppRelease(it.optString("sha256"), it.optLong("size")) }?.takeIf { it.sha256.length == 64 },
            schema = o.optInt("schema", SCHEMA),
            settingsJson = o.optJSONObject("settings")?.toString(),
        )
    }

    /** Every object in [a] that [read] accepts; anything else is skipped. */
    private fun <T> items(a: JSONArray?, read: (JSONObject) -> T): List<T> =
        a?.let { List(it.length()) { i -> it.optJSONObject(i)?.let { x -> runCatching { read(x) }.getOrNull() } }.filterNotNull() }.orEmpty()

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
