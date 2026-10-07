package dev.recica.scouter

import android.app.Activity
import android.app.ActivityManager
import android.app.admin.DevicePolicyManager
import android.content.ComponentName
import android.content.Context

/**
 * Kiosk mode, possible only while Scouter is device owner
 * (`adb shell dpm set-device-owner dev.recica.scouter/.AdminReceiver`):
 * lock task (Home, Recents and the notification shade are blocked) and no
 * status bar. The screen schedule
 * still applies: kiosk keeps people in the app, it does not keep the panel lit.
 */
object Kiosk {
    private fun dpm(ctx: Context) = ctx.getSystemService(DevicePolicyManager::class.java)
    private fun admin(ctx: Context) = ComponentName(ctx, AdminReceiver::class.java)

    fun isOwner(ctx: Context) = dpm(ctx).isDeviceOwnerApp(ctx.packageName)

    fun isLocked(ctx: Context) =
        ctx.getSystemService(ActivityManager::class.java).lockTaskModeState != ActivityManager.LOCK_TASK_MODE_NONE

    /** Brings the device in line with [on]; safe to call on every resume. */
    fun apply(activity: Activity, on: Boolean) {
        if (!isOwner(activity)) return
        val dpm = dpm(activity)
        val a = admin(activity)
        if (on) {
            dpm.setLockTaskPackages(a, arrayOf(activity.packageName))
            dpm.setKeyguardDisabled(a, true)
            dpm.setStatusBarDisabled(a, true)
            if (!isLocked(activity)) activity.startLockTask()
        } else {
            unlock(activity)
        }
    }

    /**
     * Leaves kiosk mode from any state. Each step stands alone: emptying the
     * allowlist ends lock task whichever task holds it, so one failing step
     * can never leave the phone locked.
     */
    private fun unlock(ctx: Context) {
        val dpm = dpm(ctx)
        val a = admin(ctx)
        runCatching { dpm.setLockTaskPackages(a, emptyArray()) }
        runCatching { dpm.setStatusBarDisabled(a, false) }
        runCatching { dpm.clearPackagePersistentPreferredActivities(a, ctx.packageName) }
    }

    /** Gives up device ownership, e.g. before uninstalling. */
    fun release(ctx: Context) {
        if (!isOwner(ctx)) return
        unlock(ctx)
        runCatching { dpm(ctx).setKeyguardDisabled(admin(ctx), false) }
        @Suppress("DEPRECATION")
        dpm(ctx).clearDeviceOwnerApp(ctx.packageName)
    }
}
