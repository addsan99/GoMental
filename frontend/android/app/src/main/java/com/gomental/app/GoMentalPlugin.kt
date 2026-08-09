package com.gomental.app

import android.app.AlertDialog
import android.text.InputType
import android.util.Base64
import android.view.ViewGroup
import android.widget.EditText
import android.widget.LinearLayout
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import com.gomental.mobile.Core
import com.gomental.mobile.Mobile
import org.json.JSONObject
import java.io.File
import java.net.URI
import java.net.URLConnection
import java.util.concurrent.Executors

@CapacitorPlugin(name = "GoMentalNative")
class GoMentalPlugin : Plugin() {
    private val coreLock = Any()
    private val coreExecutor = Executors.newSingleThreadExecutor()
    private var core: Core? = null

    private val preferences by lazy {
        context.getSharedPreferences("gomental-mobile", 0)
    }
    private val credentials by lazy { CredentialStore(context) }

    @PluginMethod
    fun configure(call: PluginCall) {
        val remote = call.getString("remote")?.trim().orEmpty()
        val ref = call.getString("ref")?.trim().orEmpty().ifBlank { "main" }
        val uri = runCatching { URI(remote) }.getOrNull()
        if (uri == null || !uri.scheme.equals("https", ignoreCase = true) || uri.host.isNullOrBlank() || uri.userInfo != null) {
            call.reject("Repository URL must be HTTPS and must not contain credentials")
            return
        }
        val previousRemote = preferences.getString("remote", "")
        if (!previousRemote.isNullOrBlank() && previousRemote != remote) credentials.clear()
        preferences.edit().putString("remote", remote).putString("ref", ref).apply()
        val result = JSObject()
        result.put("remote", remote)
        result.put("ref", ref)
        call.resolve(result)
    }

    @PluginMethod
    fun status(call: PluginCall) = withCore(call) { active ->
        val result = JSObject(active.status())
        result.put("configuredRemote", preferences.getString("remote", ""))
        result.put("configuredRef", preferences.getString("ref", "main"))
        result.put("hasCredential", credentials.hasCredential())
        call.resolve(result)
    }

    @PluginMethod
    fun editCredential(call: PluginCall) {
        bridge.executeOnMainThread {
            val padding = (20 * context.resources.displayMetrics.density).toInt()
            val username = EditText(activity).apply {
                hint = "GitHub username (optional)"
                setSingleLine(true)
                inputType = InputType.TYPE_CLASS_TEXT
            }
            val token = EditText(activity).apply {
                hint = "Personal access token"
                setSingleLine(true)
                inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD
            }
            val fields = LinearLayout(activity).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(padding, padding / 2, padding, 0)
                addView(username, ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT))
                addView(token, ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT))
            }
            val dialog = AlertDialog.Builder(activity)
                .setTitle("Private repository access")
                .setMessage("The token is encrypted with Android Keystore and never sent to the WebView.")
                .setView(fields)
                .setPositiveButton("Save", null)
                .setNegativeButton("Cancel") { _, _ -> resolveCredentialStatus(call) }
                .setNeutralButton("Clear") { _, _ ->
                    credentials.clear()
                    resolveCredentialStatus(call)
                }
                .create()
            dialog.setOnShowListener {
                dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
                    if (token.text.isNullOrBlank()) {
                        token.error = "Token is required"
                        return@setOnClickListener
                    }
                    try {
                        credentials.save(username.text.toString(), token.text.toString())
                        dialog.dismiss()
                        resolveCredentialStatus(call)
                    } catch (error: Exception) {
                        call.reject(error.message ?: "Could not store credential", error)
                        dialog.dismiss()
                    }
                }
            }
            dialog.setOnCancelListener { resolveCredentialStatus(call) }
            dialog.show()
        }
    }

    @PluginMethod
    fun sync(call: PluginCall) = withCore(call) { active ->
        val remote = preferences.getString("remote", null)
        if (remote.isNullOrBlank()) {
            call.reject("Configure a repository before syncing")
            return@withCore
        }
        val credential = credentials.read()
        val request = JSONObject()
            .put("remote", remote)
            .put("ref", preferences.getString("ref", "main"))
            .put("username", credential?.username ?: "")
            .put("token", credential?.token ?: "")
        resolveJSON(call, active.sync(request.toString()))
    }

    @PluginMethod
    fun listNotes(call: PluginCall) = withCore(call) { active ->
        val query = call.getObject("query") ?: JSObject()
        resolveJSON(call, active.listNotes(query.toString()))
    }

    @PluginMethod
    fun search(call: PluginCall) = withCore(call) { active ->
        val query = call.getObject("query") ?: JSObject()
        resolveJSON(call, active.search(query.toString()))
    }

    @PluginMethod
    fun readNote(call: PluginCall) = withCore(call) { active ->
        val id = call.getString("id")?.trim()
        if (id.isNullOrEmpty()) {
            call.reject("Note ID is required")
            return@withCore
        }
        resolveJSON(call, active.readNote(id))
    }

    @PluginMethod
    fun loadAsset(call: PluginCall) = withCore(call) { active ->
        val noteId = call.getString("noteId")?.trim()
        val path = call.getString("path")?.trim()
        if (noteId.isNullOrEmpty() || path.isNullOrEmpty()) {
            call.reject("Note ID and asset path are required")
            return@withCore
        }
        val bytes = active.loadAsset(noteId, path)
        val mimeType = URLConnection.guessContentTypeFromName(path) ?: "application/octet-stream"
        val dataURL = "data:$mimeType;base64," + Base64.encodeToString(bytes, Base64.NO_WRAP)
        val result = JSObject()
        result.put("dataUrl", dataURL)
        call.resolve(result)
    }

    @PluginMethod
    fun cancel(call: PluginCall) {
        synchronized(coreLock) { core?.cancel() }
        call.resolve()
    }

    @PluginMethod
    fun close(call: PluginCall) {
        coreExecutor.execute {
            try {
                closeCore()
                call.resolve()
            } catch (error: Exception) {
                call.reject(error.message ?: "Could not close notes core", error)
            }
        }
    }

    override fun handleOnDestroy() {
        synchronized(coreLock) { core?.cancel() }
        coreExecutor.execute { runCatching { closeCore() } }
        coreExecutor.shutdown()
        super.handleOnDestroy()
    }

    private fun withCore(call: PluginCall, operation: (Core) -> Unit) {
        coreExecutor.execute {
            try {
                operation(ensureCore())
            } catch (error: Exception) {
                call.reject(error.message ?: "Native notes operation failed", error)
            }
        }
    }

    private fun ensureCore(): Core = synchronized(coreLock) {
        core ?: run {
            val repository = File(context.filesDir, "repository")
            val data = File(context.filesDir, "derived-data")
            val config = JSONObject()
                .put("repositoryPath", repository.absolutePath)
                .put("dataPath", data.absolutePath)
            Mobile.open(config.toString()).also { core = it }
        }
    }

    private fun closeCore() = synchronized(coreLock) {
        core?.close()
        core = null
    }

    private fun resolveJSON(call: PluginCall, json: String) {
        call.resolve(JSObject(json))
    }

    private fun resolveCredentialStatus(call: PluginCall) {
        val result = JSObject()
        result.put("hasCredential", credentials.hasCredential())
        call.resolve(result)
    }
}
