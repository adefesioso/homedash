package net.homedash.mobile

import android.app.AppOpsManager
import android.app.KeyguardManager
import android.app.usage.UsageEvents
import android.app.usage.UsageStatsManager
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.BatteryManager
import android.os.Build
import android.os.Environment
import android.os.PowerManager
import android.os.Process
import android.os.StatFs
import org.json.JSONObject

/**
 * Reads whatever the phone can currently say about itself, in the shape
 * MobileStats.svelte reads on the panel: battery, storage, screen,
 * network, foreground app. Nothing here needs a runtime permission
 * prompt except the foreground app, which is silently left out until
 * usage access is granted by hand.
 */
object StatsCollector {
    fun collect(context: Context): JSONObject {
        val out = JSONObject()
        battery(context)?.let { (level, charging) ->
            out.put("battery", level)
            out.put("charging", charging)
        }
        val stat = StatFs(Environment.getDataDirectory().path)
        out.put("storageTotal", stat.totalBytes)
        out.put("storageFree", stat.availableBytes)
        out.put("screen", screenState(context))
        out.put("network", networkType(context))
        foregroundApp(context)?.let { out.put("foregroundApp", it) }
        return out
    }

    private fun battery(context: Context): Pair<Int, Boolean>? {
        val status = context.registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED)) ?: return null
        val level = status.getIntExtra(BatteryManager.EXTRA_LEVEL, -1)
        val scale = status.getIntExtra(BatteryManager.EXTRA_SCALE, -1)
        if (level < 0 || scale <= 0) return null
        val plugged = status.getIntExtra(BatteryManager.EXTRA_PLUGGED, 0)
        return Pair((level * 100) / scale, plugged != 0)
    }

    private fun screenState(context: Context): String {
        val pm = context.getSystemService(Context.POWER_SERVICE) as PowerManager
        if (!pm.isInteractive) return "off"
        val km = context.getSystemService(Context.KEYGUARD_SERVICE) as KeyguardManager
        return if (km.isKeyguardLocked) "locked" else "on"
    }

    private fun networkType(context: Context): String {
        val cm = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        val caps = cm.getNetworkCapabilities(cm.activeNetwork) ?: return "offline"
        return when {
            caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
            else -> "other"
        }
    }

    @Suppress("DEPRECATION")
    fun hasUsageAccess(context: Context): Boolean {
        val appOps = context.getSystemService(Context.APP_OPS_SERVICE) as AppOpsManager
        val mode = appOps.checkOpNoThrow(AppOpsManager.OPSTR_GET_USAGE_STATS, Process.myUid(), context.packageName)
        return mode == AppOpsManager.MODE_ALLOWED
    }

    @Suppress("DEPRECATION")
    private fun foregroundApp(context: Context): String? {
        if (!hasUsageAccess(context)) return null
        val usm = context.getSystemService(Context.USAGE_STATS_SERVICE) as UsageStatsManager
        val end = System.currentTimeMillis()
        val events = usm.queryEvents(end - 60_000, end)
        val resumedType = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            UsageEvents.Event.ACTIVITY_RESUMED
        } else {
            UsageEvents.Event.MOVE_TO_FOREGROUND
        }
        var last: String? = null
        val event = UsageEvents.Event()
        while (events.hasNextEvent()) {
            events.getNextEvent(event)
            if (event.eventType == resumedType) last = event.packageName
        }
        return last
    }
}
