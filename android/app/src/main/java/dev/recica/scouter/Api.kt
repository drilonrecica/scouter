package dev.recica.scouter

import android.content.Context
import android.os.Handler
import android.os.Looper
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.Executors

/** Writes to the aggregator. Reads arrive over the stream in [StreamService]. */
object Api {
    private val io = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    /**
     * Sends the whole settings object (PUT replaces it). The screen updates at
     * once from the local copy; the server's answer arrives over the stream.
     * [done] runs on the main thread with null on success, else an error message.
     */
    fun saveSettings(ctx: Context, next: Settings, done: (String?) -> Unit = {}) {
        val prefs = Prefs(ctx)
        Hub.applySettings(next)
        io.execute {
            val error = runCatching {
                val c = URL(prefs.url + "/v1/settings").openConnection() as HttpURLConnection
                try {
                    c.requestMethod = "PUT"
                    c.connectTimeout = 10_000
                    c.readTimeout = 10_000
                    c.doOutput = true
                    c.setRequestProperty("Authorization", "Bearer " + prefs.token)
                    c.setRequestProperty("Content-Type", "application/json")
                    c.outputStream.use { it.write(next.toJson().toByteArray()) }
                    when (val code = c.responseCode) {
                        204, 200 -> null
                        404, 405 -> "server too old for settings (redeploy the aggregator)"
                        else -> "HTTP $code: " + (c.errorStream?.bufferedReader()?.readText()?.trim().orEmpty())
                    }
                } finally {
                    c.disconnect()
                }
            }.getOrElse { it.message ?: it.javaClass.simpleName }
            main.post { done(error) }
        }
    }
}
