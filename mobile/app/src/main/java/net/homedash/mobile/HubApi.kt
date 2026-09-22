package net.homedash.mobile

import android.os.Build
import org.json.JSONObject
import java.io.OutputStreamWriter
import java.net.HttpURLConnection
import java.net.URL
import java.nio.charset.StandardCharsets

/**
 * The two calls the hub's mobile.go expects, matching its own naming:
 * pairing spends a code at `/enroll/{code}` for a device key, and
 * status pushes it to `/mobile/{host}/status` as a bearer token. Both
 * run over plain http, on whatever LAN address the phone was pointed
 * at — there is no route to the hub beyond that.
 */
object HubApi {
    class PairResult(val hostRef: String, val deviceKey: String)

    class HttpError(val code: Int, message: String) : Exception(message)

    fun pair(hubAddress: String, code: String, facts: JSONObject): PairResult {
        val body = JSONObject()
            .put("model", Build.MODEL)
            .put("androidVersion", Build.VERSION.RELEASE)
            .put("facts", facts)
        val conn = open("http://$hubAddress/enroll/$code", "POST")
        try {
            writeJson(conn, body)
            checkOk(conn)
            val res = JSONObject(conn.inputStream.bufferedReader().readText())
            val ref = if (res.has("name") && !res.isNull("name")) res.getString("name") else res.getLong("id").toString()
            return PairResult(ref, res.getString("deviceKey"))
        } finally {
            conn.disconnect()
        }
    }

    fun pushStatus(hubAddress: String, hostRef: String, deviceKey: String, facts: JSONObject) {
        val conn = open("http://$hubAddress/mobile/$hostRef/status", "POST")
        try {
            conn.setRequestProperty("Authorization", "Bearer $deviceKey")
            writeJson(conn, facts)
            checkOk(conn)
        } finally {
            conn.disconnect()
        }
    }

    private fun open(url: String, method: String): HttpURLConnection {
        val conn = URL(url).openConnection() as HttpURLConnection
        conn.requestMethod = method
        conn.doOutput = true
        conn.setRequestProperty("Content-Type", "application/json")
        conn.connectTimeout = 10_000
        conn.readTimeout = 10_000
        return conn
    }

    private fun writeJson(conn: HttpURLConnection, body: JSONObject) {
        OutputStreamWriter(conn.outputStream, StandardCharsets.UTF_8).use { it.write(body.toString()) }
    }

    private fun checkOk(conn: HttpURLConnection) {
        val code = conn.responseCode
        if (code !in 200..299) {
            val text = (conn.errorStream ?: conn.inputStream)?.bufferedReader()?.readText().orEmpty()
            throw HttpError(code, text.ifBlank { "hub returned $code" })
        }
    }
}
