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
)

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
        )
    }

    private fun project(o: JSONObject) = Project(
        name = o.getString("name"),
        fullName = o.getString("full_name"),
        defaultBranch = o.optString("default_branch"),
        pushedAt = instant(o, "pushed_at"),
        ci = o.optJSONObject("ci")?.let(::run),
        latest = o.optJSONObject("latest")?.let(::run),
        power = if (o.has("power")) o.getInt("power") else null,
        openPRs = o.optInt("open_prs"),
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
