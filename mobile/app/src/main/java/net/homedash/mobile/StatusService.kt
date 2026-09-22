package net.homedash.mobile

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/**
 * The heartbeat runs backwards for a phone (docs/pooling/mobile.md): the
 * hub never dials in, so this loop reports out, once a minute, for as
 * long as the notification below is showing. A 401 or 404 means the
 * hub no longer recognizes this device key or this host — pairing was
 * undone on the hub's side — so the loop clears its own state and stops
 * rather than retrying forever.
 */
class StatusService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var loop: Job? = null

    override fun onCreate() {
        super.onCreate()
        val notification = buildNotification("Watching for the hub")
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(NOTIF_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        } else {
            startForeground(NOTIF_ID, notification)
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (loop?.isActive != true) loop = scope.launch { report() }
        return START_STICKY
    }

    override fun onDestroy() {
        loop?.cancel()
        scope.cancel()
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    private suspend fun report() {
        val prefs = Prefs(applicationContext)
        while (scope.isActive) {
            val hub = prefs.hubAddress
            val ref = prefs.hostRef
            val key = prefs.deviceKey
            if (hub == null || ref == null || key == null) {
                stopSelf()
                return
            }
            try {
                HubApi.pushStatus(hub, ref, key, StatsCollector.collect(applicationContext))
                updateNotification("Last reported ${timeLabel()}")
            } catch (e: HubApi.HttpError) {
                if (e.code == 401 || e.code == 404) {
                    prefs.clear()
                    updateNotification("Unpaired on the hub — open the app to pair again")
                    stopSelf()
                    return
                }
                updateNotification("Hub error, retrying in a minute")
            } catch (e: Exception) {
                updateNotification("Hub unreachable, retrying in a minute")
            }
            delay(60_000)
        }
    }

    private fun timeLabel() = SimpleDateFormat("HH:mm", Locale.getDefault()).format(Date())

    private fun buildNotification(text: String): Notification {
        val channelId = ensureChannel()
        val openApp = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE
        )
        return Notification.Builder(this, channelId)
            .setContentTitle(getString(R.string.app_name))
            .setContentText(text)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentIntent(openApp)
            .setOngoing(true)
            .build()
    }

    private fun updateNotification(text: String) {
        val nm = getSystemService(NotificationManager::class.java)
        nm.notify(NOTIF_ID, buildNotification(text))
    }

    private fun ensureChannel(): String {
        val id = "status"
        val nm = getSystemService(NotificationManager::class.java)
        if (nm.getNotificationChannel(id) == null) {
            nm.createNotificationChannel(NotificationChannel(id, "Status reporting", NotificationManager.IMPORTANCE_LOW))
        }
        return id
    }

    companion object {
        private const val NOTIF_ID = 1

        fun start(context: Context) {
            // minSdk is 26 (O), so a foreground service start is always available.
            context.startForegroundService(Intent(context, StatusService::class.java))
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, StatusService::class.java))
        }
    }
}
