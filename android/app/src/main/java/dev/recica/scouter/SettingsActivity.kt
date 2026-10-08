package dev.recica.scouter

import android.app.Activity
import android.app.AlertDialog
import android.app.TimePickerDialog
import android.graphics.Color
import android.graphics.Typeface
import android.net.Uri
import android.os.Bundle
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.CheckBox
import android.widget.LinearLayout
import android.widget.NumberPicker
import android.widget.RadioButton
import android.widget.RadioGroup
import android.widget.ScrollView
import android.widget.Switch
import android.widget.TextView
import android.widget.Toast

/**
 * Edits the server's settings from the phone (opened by long-pressing the
 * HUD's top strip). Works on a draft and sends the whole object on SAVE; the
 * server validates it and every screen picks it up from the stream. The
 * server URL and token stay adb-only on purpose.
 */
class SettingsActivity : Activity() {
    private lateinit var draft: Settings
    private lateinit var body: LinearLayout
    private val d by lazy { resources.displayMetrics.density }
    private val mono by lazy { resources.getFont(R.font.share_tech_mono) }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val s = Hub.state
        if (s == null) {
            Toast.makeText(this, "Not connected yet: settings come from the server", Toast.LENGTH_LONG).show()
            finish()
            return
        }
        if (!s.hasSettings) {
            Toast.makeText(this, "The server is too old for settings: redeploy the aggregator", Toast.LENGTH_LONG).show()
            finish()
            return
        }
        draft = s.settings
        body = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(px(28), px(20), px(28), px(28))
        }
        setContentView(ScrollView(this).apply { setBackgroundColor(Color.BLACK); addView(body) })
        build(s)
    }

    private fun px(dp: Int) = (dp * d).toInt()

    // ---- layout -------------------------------------------------------------------

    private fun build(s: DashState) {
        body.removeAllViews()
        body.addView(LinearLayout(this).apply {
            gravity = Gravity.CENTER_VERTICAL
            addView(label("◤ SCOUTER SETTINGS", 20f, Hud.CHROME_TEXT), LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f))
            addView(button("CANCEL", primary = false) { finish() })
            addView(View(context), LinearLayout.LayoutParams(px(12), 1))
            addView(button("SAVE") { save() })
        })

        section("FOCUS")
        val group = RadioGroup(this)
        for ((mode, text) in listOf("latest" to "Latest push", "pinned" to "Pinned project", "rotate" to "Rotate favorites")) {
            group.addView(RadioButton(this).apply {
                this.text = text
                setTextColor(PRIMARY)
                id = View.generateViewId()
                isChecked = draft.focusMode == mode
                setOnClickListener {
                    draft = draft.copy(focusMode = mode, pinned = if (mode == "pinned" && draft.pinned.isEmpty()) Logic.focus(s)?.fullName.orEmpty() else draft.pinned)
                    build(s)
                }
            })
        }
        body.addView(group)
        if (draft.focusMode == "pinned") hint("Pinned: ${draft.pinned.ifEmpty { "—" }} (tap a Grid tile or long-press Focus to change)")
        if (draft.focusMode == "rotate") number("Rotate every (minutes)", draft.rotateMinutes, 1, 60) { draft = draft.copy(rotateMinutes = it) }

        section("SCREEN")
        val (from, to) = draft.days.split("-").map { it.toInt() }
        number("From day (1 = Monday)", from, 1, 7) { draft = draft.copy(days = "$it-${draft.days.substringAfter('-')}") }
        number("To day", to, 1, 7) { draft = draft.copy(days = "${draft.days.substringBefore('-')}-$it") }
        timeRow("On from", draft.on) { draft = draft.copy(on = it); build(s) }
        timeRow("Off at", draft.off) { draft = draft.copy(off = it); build(s) }
        number("Alerts for failures younger than (hours)", draft.alertHours, 1, 72) { draft = draft.copy(alertHours = it) }
        toggle("Wave to wake (peek at night for 20 s)", draft.wave) { draft = draft.copy(wave = it) }
        toggle("Morning briefing when the screen turns on", draft.briefing) { draft = draft.copy(briefing = it) }

        section("KIOSK")
        val owner = Kiosk.isOwner(this)
        toggle("Kiosk mode", draft.kiosk, enabled = owner) { draft = draft.copy(kiosk = it) }
        hint(
            if (owner) "Locks the phone into Scouter: no Home, Recents or notification shade. The screen still sleeps outside the hours above."
            else "Needs device owner: adb shell dpm set-device-owner dev.recica.scouter/.AdminReceiver (see README).",
        )

        section("BACKGROUND")
        toggle("Ki aura (edge glow in the status colour)", draft.aura) { draft = draft.copy(aura = it) }
        toggle("Star field (re-seeded every minute)", draft.stars) { draft = draft.copy(stars = it) }
        toggle("Hex lens mesh", draft.mesh) { draft = draft.copy(mesh = it) }

        section("CONNECTION")
        val host = runCatching { Uri.parse(Prefs(this).url).host }.getOrNull() ?: "—"
        hint("Server: $host · ${if (Hub.connected) "connected" else "no signal"} · document v${s.version} · app ${packageManager.getPackageInfo(packageName, 0).versionName}")
        hint("Server and token can only be changed over adb.")

        section("PROJECTS")
        hint("★ favorites are always tracked, shown first and used by Rotate. Hidden repos are never tracked.")
        val names = (draft.favorites + s.available + draft.hidden).distinct()
        val ordered = draft.favorites + names.filter { it !in draft.favorites }.sortedBy { it.lowercase() }
        for (repo in ordered) projectRow(repo)

        if (owner) {
            section("DEVICE OWNER")
            hint("Scouter is device owner. Release it before uninstalling the app or handing the phone on.")
            body.addView(button("RELEASE DEVICE OWNER", primary = false) {
                AlertDialog.Builder(this)
                    .setTitle("Release device owner?")
                    .setMessage("Kiosk mode stops working until it is set again over adb.")
                    .setPositiveButton("Release") { _, _ ->
                        Kiosk.release(this)
                        Toast.makeText(this, "Device owner released", Toast.LENGTH_LONG).show()
                        build(s)
                    }
                    .setNegativeButton("Cancel", null)
                    .show()
            })
        }
    }

    private fun projectRow(repo: String) {
        val fav = repo in draft.favorites
        val hidden = repo in draft.hidden
        body.addView(LinearLayout(this).apply {
            gravity = Gravity.CENTER_VERTICAL
            setPadding(0, px(4), 0, px(4))
            addView(TextView(context).apply {
                text = if (fav) "★" else "☆"
                textSize = 24f
                setTextColor(if (fav) STAR else SECONDARY)
                setPadding(0, 0, px(14), 0)
                contentDescription = "favorite $repo"
                setOnClickListener {
                    draft = if (fav) draft.copy(favorites = draft.favorites - repo, focusMode = fixMode(draft.favorites - repo))
                    else draft.copy(favorites = draft.favorites + repo, hidden = draft.hidden - repo)
                    build(Hub.state ?: return@setOnClickListener)
                }
            })
            addView(TextView(context).apply {
                text = repo.substringAfter('/')
                textSize = 18f
                setTextColor(if (hidden) DIM else PRIMARY)
            }, LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f))
            if (fav && draft.favorites.indexOf(repo) > 0) {
                addView(button("↑", primary = false) {
                    val i = draft.favorites.indexOf(repo)
                    draft = draft.copy(favorites = draft.favorites.toMutableList().apply { add(i - 1, removeAt(i)) })
                    build(Hub.state ?: return@button)
                })
            }
            addView(CheckBox(context).apply {
                text = "hide"
                setTextColor(SECONDARY)
                isChecked = hidden
                setOnCheckedChangeListener { _, on ->
                    draft = if (on) draft.copy(hidden = draft.hidden + repo, favorites = draft.favorites - repo, focusMode = fixMode(draft.favorites - repo))
                    else draft.copy(hidden = draft.hidden - repo)
                    build(Hub.state ?: return@setOnCheckedChangeListener)
                }
            })
        })
    }

    /** Rotate without favorites has nothing to rotate through: fall back to latest. */
    private fun fixMode(favs: List<String>) = if (draft.focusMode == "rotate" && favs.isEmpty()) "latest" else draft.focusMode

    // ---- widgets ------------------------------------------------------------------

    private fun label(text: String, size: Float, color: Int) = TextView(this).apply {
        this.text = text
        textSize = size
        setTextColor(color)
        typeface = mono
        letterSpacing = 0.08f
    }

    private fun section(title: String) {
        body.addView(label(title, 15f, Hud.CHROME_TEXT).apply { setPadding(0, px(26), 0, px(8)) })
        body.addView(View(this).apply { setBackgroundColor(Hud.CHROME_DIM) }, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, px(1)))
    }

    private fun hint(text: String) = body.addView(TextView(this).apply {
        this.text = text
        textSize = 14f
        setTextColor(SECONDARY)
        setPadding(0, px(6), 0, px(6))
    })

    private fun button(text: String, primary: Boolean = true, onClick: () -> Unit) = Button(this).apply {
        this.text = text
        typeface = Typeface.create(mono, Typeface.BOLD)
        setTextColor(if (primary) Color.BLACK else Hud.CHROME_TEXT)
        setBackgroundColor(if (primary) Hud.CHROME_TEXT else Color.TRANSPARENT)
        setOnClickListener { onClick() }
    }

    private fun toggle(text: String, value: Boolean, enabled: Boolean = true, onChange: (Boolean) -> Unit) = body.addView(Switch(this).apply {
        this.text = text
        textSize = 17f
        setTextColor(if (enabled) PRIMARY else DIM)
        isChecked = value
        isEnabled = enabled
        setPadding(0, px(8), 0, px(8))
        setOnCheckedChangeListener { _, on -> onChange(on) }
    })

    private fun number(text: String, value: Int, min: Int, max: Int, onChange: (Int) -> Unit) = body.addView(LinearLayout(this).apply {
        gravity = Gravity.CENTER_VERTICAL
        addView(TextView(context).apply { this.text = text; textSize = 17f; setTextColor(PRIMARY) }, LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f))
        addView(NumberPicker(context).apply {
            minValue = min
            maxValue = max
            this.value = value.coerceIn(min, max)
            wrapSelectorWheel = false
            setOnValueChangedListener { _, _, v -> onChange(v) }
        }, LinearLayout.LayoutParams(px(90), px(120)))
    })

    private fun timeRow(text: String, value: String, onChange: (String) -> Unit) = body.addView(LinearLayout(this).apply {
        gravity = Gravity.CENTER_VERTICAL
        setPadding(0, px(4), 0, px(4))
        addView(TextView(context).apply { this.text = text; textSize = 17f; setTextColor(PRIMARY) }, LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f))
        addView(button(value, primary = false) {
            val (h, m) = value.split(":").map { it.toInt() }
            TimePickerDialog(this@SettingsActivity, { _, hh, mm -> onChange("%02d:%02d".format(hh, mm)) }, h, m, true).show()
        })
    })

    // ---- save -----------------------------------------------------------------------

    private fun save() {
        if (draft.on >= draft.off) {
            Toast.makeText(this, "The screen must turn on before it turns off", Toast.LENGTH_LONG).show()
            return
        }
        Api.saveSettings(this, draft) { err ->
            if (err == null) {
                Toast.makeText(this, "Saved", Toast.LENGTH_SHORT).show()
                finish()
            } else {
                Toast.makeText(this, "Not saved: $err", Toast.LENGTH_LONG).show()
            }
        }
    }

    companion object {
        private const val PRIMARY = 0xFFE8E8E8.toInt()
        private const val SECONDARY = 0xFF8C8C8C.toInt()
        private const val DIM = 0xFF4A4A4A.toInt()
        private const val STAR = 0xFFFFC94D.toInt()
    }
}
