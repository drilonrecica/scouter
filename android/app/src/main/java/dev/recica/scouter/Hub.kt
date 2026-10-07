package dev.recica.scouter

import android.content.Context
import android.os.Handler
import android.os.Looper
import java.time.Instant

/**
 * In-process state shared by the service (writer) and the activity (reader).
 * All access happens on the main thread; the stream thread posts into it.
 */
object Hub {
    private val main = Handler(Looper.getMainLooper())
    private val listeners = mutableSetOf<() -> Unit>()

    var state: DashState? = null
        private set
    var connected = false
        private set
    var disconnectedSince: Instant? = Instant.now()
        private set

    /** Screen should stay on: inside the schedule, or woken by a fresh alert. */
    var wantScreenOn = false
        set(v) {
            if (field != v) { field = v; notifyListeners() }
        }

    /** When each alert was first seen on this phone: drives card (fresh) vs banner. */
    val alertSeenAt = mutableMapOf<String, Instant>()

    fun postState(s: DashState) = main.post {
        state = s
        val ids = s.alerts.map { it.id }.toSet()
        alertSeenAt.keys.retainAll(ids)
        for (id in ids) alertSeenAt.putIfAbsent(id, Instant.now())
        setConnectedLocked(true)
        notifyListeners()
    }

    /** Optimistic local copy of new settings until the server's document confirms them. */
    fun applySettings(next: Settings) {
        state = state?.copy(settings = next)
        notifyListeners()
    }

    fun postConnected(c: Boolean) = main.post {
        if (c != connected) { setConnectedLocked(c); notifyListeners() }
    }

    private fun setConnectedLocked(c: Boolean) {
        if (c == connected) return
        connected = c
        disconnectedSince = if (c) null else Instant.now()
    }

    fun listen(l: () -> Unit) { listeners += l }
    fun unlisten(l: () -> Unit) { listeners -= l }
    fun notifyListeners() = listeners.toList().forEach { it() }
}

/** Configuration and the little state the phone owns (pin, dismissed alerts). */
class Prefs(ctx: Context) {
    private val p = ctx.getSharedPreferences("scouter", Context.MODE_PRIVATE)

    var url: String
        get() = p.getString("url", "")!!.trimEnd('/')
        set(v) = p.edit().putString("url", v).apply()
    var token: String
        get() = p.getString("token", "")!!
        set(v) = p.edit().putString("token", v).apply()
    var schedule: String
        get() = p.getString("schedule", "1-5 09:00-19:00")!!
        set(v) = p.edit().putString("schedule", v).apply()
    var dismissed: Set<String>
        get() = p.getStringSet("dismissed", emptySet())!!
        set(v) = p.edit().putStringSet("dismissed", v).apply()

    val configured get() = url.isNotEmpty() && token.isNotEmpty()
}
