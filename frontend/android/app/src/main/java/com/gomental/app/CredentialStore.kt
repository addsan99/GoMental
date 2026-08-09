package com.gomental.app

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

data class GitCredential(val username: String, val token: String)

class CredentialStore(context: Context) {
    private val preferences = context.getSharedPreferences("gomental-credentials", Context.MODE_PRIVATE)
    private val keyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }

    fun hasCredential(): Boolean = preferences.contains(TOKEN) && preferences.contains(IV)

    fun save(username: String, token: String) {
        require(token.isNotBlank()) { "Token is required" }
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, secretKey())
        val encrypted = cipher.doFinal(token.toByteArray(Charsets.UTF_8))
        preferences.edit()
            .putString(USERNAME, username.trim().ifBlank { "x-access-token" })
            .putString(IV, Base64.encodeToString(cipher.iv, Base64.NO_WRAP))
            .putString(TOKEN, Base64.encodeToString(encrypted, Base64.NO_WRAP))
            .apply()
    }

    fun read(): GitCredential? {
        val encodedIV = preferences.getString(IV, null) ?: return null
        val encodedToken = preferences.getString(TOKEN, null) ?: return null
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(
            Cipher.DECRYPT_MODE,
            secretKey(),
            GCMParameterSpec(128, Base64.decode(encodedIV, Base64.NO_WRAP)),
        )
        val token = cipher.doFinal(Base64.decode(encodedToken, Base64.NO_WRAP)).toString(Charsets.UTF_8)
        return GitCredential(preferences.getString(USERNAME, "x-access-token") ?: "x-access-token", token)
    }

    fun clear() {
        preferences.edit().clear().apply()
    }

    private fun secretKey(): SecretKey {
        val existing = keyStore.getKey(KEY_ALIAS, null) as? SecretKey
        if (existing != null) return existing
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        generator.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return generator.generateKey()
    }

    private companion object {
        const val KEY_ALIAS = "gomental.git.token"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val USERNAME = "username"
        const val TOKEN = "token"
        const val IV = "iv"
    }
}
