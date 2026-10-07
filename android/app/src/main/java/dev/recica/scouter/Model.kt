package dev.recica.scouter

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
)

data class Project(
    val name: String,
    val fullName: String,
    val defaultBranch: String,
    val pushedAt: Instant?,
    val ci: Run?,
    val latest: Run?,
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
        return DashState(o.optLong("version"), o.optString("focus"), projects, alerts, sources)
    }

    private fun project(o: JSONObject) = Project(
        name = o.getString("name"),
        fullName = o.getString("full_name"),
        defaultBranch = o.optString("default_branch"),
        pushedAt = instant(o, "pushed_at"),
        ci = o.optJSONObject("ci")?.let(::run),
        latest = o.optJSONObject("latest")?.let(::run),
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
