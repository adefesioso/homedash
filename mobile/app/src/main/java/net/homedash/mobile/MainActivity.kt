package net.homedash.mobile

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import android.view.View
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import com.google.android.material.button.MaterialButton
import com.google.android.material.textfield.TextInputEditText
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * One screen, two states: a pairing form (hub address + code) before
 * Prefs.isPaired, a status readout with Unpair after. Pairing itself and
 * the recurring status push are entirely StatusService's job once this
 * activity has stored a device key — this screen never talks to the hub
 * on its own after that first call.
 */
class MainActivity : AppCompatActivity() {
    private lateinit var prefs: Prefs
    private val scope = CoroutineScope(Dispatchers.Main + SupervisorJob())

    private val requestNotifications =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { /* service still runs without it */ }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
        prefs = Prefs(this)

        findViewById<MaterialButton>(R.id.pairButton).setOnClickListener { pair() }
        findViewById<MaterialButton>(R.id.usageAccessButton).setOnClickListener {
            startActivity(Intent(Settings.ACTION_USAGE_ACCESS_SETTINGS))
        }
        findViewById<MaterialButton>(R.id.unpairButton).setOnClickListener { unpair() }

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            requestNotifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }

    override fun onResume() {
        super.onResume()
        render()
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private fun render() {
        val paired = prefs.isPaired
        findViewById<View>(R.id.pairingGroup).visibility = if (paired) View.GONE else View.VISIBLE
        findViewById<View>(R.id.pairedGroup).visibility = if (paired) View.VISIBLE else View.GONE
        if (!paired) return

        findViewById<TextView>(R.id.pairedAs).text = "Paired as ${prefs.hostRef} on ${prefs.hubAddress}"
        findViewById<TextView>(R.id.pairedStatus).text = if (StatsCollector.hasUsageAccess(this)) {
            "Reporting once a minute, including the foreground app."
        } else {
            "Reporting once a minute. Grant usage access to also report the foreground app."
        }
        StatusService.start(this)
    }

    private fun pair() {
        val hub = findViewById<TextInputEditText>(R.id.hubAddressInput).text?.toString()?.trim().orEmpty()
        val code = findViewById<TextInputEditText>(R.id.codeInput).text?.toString()?.trim().orEmpty()
        val error = findViewById<TextView>(R.id.pairError)
        error.visibility = View.GONE
        if (hub.isEmpty() || code.isEmpty()) {
            showError(error, "Enter both the hub address and the code.")
            return
        }

        val button = findViewById<MaterialButton>(R.id.pairButton)
        button.isEnabled = false
        scope.launch {
            try {
                val facts = withContext(Dispatchers.Default) { StatsCollector.collect(this@MainActivity) }
                val result = withContext(Dispatchers.IO) { HubApi.pair(hub, code, facts) }
                prefs.hubAddress = hub
                prefs.hostRef = result.hostRef
                prefs.deviceKey = result.deviceKey
                render()
            } catch (e: Exception) {
                showError(error, "Couldn't pair: ${e.message ?: "check the address and code"}")
            } finally {
                button.isEnabled = true
            }
        }
    }

    private fun showError(view: TextView, text: String) {
        view.text = text
        view.visibility = View.VISIBLE
    }

    private fun unpair() {
        StatusService.stop(this)
        prefs.clear()
        render()
    }
}
