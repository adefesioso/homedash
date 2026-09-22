package net.homedash.mobile

import android.content.Context

/**
 * The phone's whole pairing state: the hub address it was pointed at,
 * the host name/id the pairing response handed back, and the device
 * key that is the phone's only credential from then on. Cleared on
 * Unpair, or by the status loop itself once the hub refuses the key.
 */
class Prefs(context: Context) {
    private val sp = context.getSharedPreferences("pairing", Context.MODE_PRIVATE)

    var hubAddress: String?
        get() = sp.getString("hubAddress", null)
        set(value) = sp.edit().putString("hubAddress", value).apply()

    var hostRef: String?
        get() = sp.getString("hostRef", null)
        set(value) = sp.edit().putString("hostRef", value).apply()

    var deviceKey: String?
        get() = sp.getString("deviceKey", null)
        set(value) = sp.edit().putString("deviceKey", value).apply()

    val isPaired: Boolean
        get() = !hubAddress.isNullOrBlank() && !hostRef.isNullOrBlank() && !deviceKey.isNullOrBlank()

    fun clear() = sp.edit().clear().apply()
}
