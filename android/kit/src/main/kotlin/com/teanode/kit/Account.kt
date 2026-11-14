package com.teanode.kit

import kotlinx.serialization.Serializable

/** The app's own return address, which it registers with every server (internal/api/v1api/apioauth/connect.go). */
const val appRedirectAddress = "com.teanode.app:/oauth"

/** The URL scheme of [appRedirectAddress]. */
const val appRedirectScheme = "com.teanode.app"

/** Account is one server the app is connected to, and its sign-in there. */
@Serializable
data class Account(
    val server: ServerAddress,
    val clientId: String,
    val accessToken: String,
    val refreshToken: String,
    val expiresAtEpochMS: Long,
    val username: String,
    val cachedRecordingUserId: String? = null,
) {
    /** Which sign-in this is: one person on one server. Signing in again as the same person replaces it. */
    val accountKey: String get() = "$server|${username.lowercase()}"
}

/**
 * AccountRing is every server the app is signed in to, and the one in use. The app and everything in it acts as the
 * one in use.
 */
@Serializable
data class AccountRing(val accounts: List<Account> = emptyList(), val activeKey: String? = null) {
    /** The sign-in in use; the first when none is chosen. */
    val active: Account? get() = accounts.firstOrNull { account -> account.accountKey == activeKey } ?: accounts.firstOrNull()

    /** Keeps a sign-in, replacing the one for the same person on the same server; the first one kept is in use. */
    fun put(account: Account): AccountRing {
        val index = accounts.indexOfFirst { kept -> kept.accountKey == account.accountKey }
        val kept = if (index >= 0) accounts.toMutableList().also { list -> list[index] = account } else accounts + account
        val ring = AccountRing(kept, activeKey)
        return if (activeKey == null || ring.active?.accountKey != activeKey) AccountRing(kept, ring.active?.accountKey) else ring
    }

    /** Puts a sign-in to use; nothing happens for one not kept. */
    fun activate(accountKey: String): AccountRing =
        if (accounts.any { account -> account.accountKey == accountKey }) AccountRing(accounts, accountKey) else this

    /** Forgets a sign-in. When it was the one in use, the next kept one is. */
    fun remove(accountKey: String): AccountRing {
        val kept = accounts.filter { account -> account.accountKey != accountKey }
        val ring = AccountRing(kept, activeKey)
        return if (activeKey == accountKey || ring.active?.accountKey != activeKey) AccountRing(kept, kept.firstOrNull()?.accountKey) else ring
    }
}

/** AccountStore keeps an account between launches: encrypted storage in the app, memory in a test. */
interface AccountStore {
    fun save(account: Account)
    fun load(): Account?

    /** The kept sign-in of one account, whether or not it is the one in use. */
    fun load(accountKey: String): Account? = load()?.takeIf { account -> account.accountKey == accountKey }

    fun delete()
}

/** SecretStore keeps named secrets on this phone. */
interface SecretStore {
    fun read(name: String): ByteArray?
    fun write(name: String, secret: ByteArray)
    fun delete(name: String)
}

/**
 * SecretAccountStore keeps the sign-ins in a [SecretStore]: every server the app is signed in to, and the one in use,
 * which is what load and delete act on.
 */
class SecretAccountStore(private val secrets: SecretStore) : AccountStore {
    /** Keeps a sign-in, renewed or new, without changing the one in use. */
    @Synchronized
    override fun save(account: Account) = write(accounts().put(account))

    @Synchronized
    override fun load(): Account? = accounts().active

    @Synchronized
    override fun load(accountKey: String): Account? = accounts().accounts.firstOrNull { account -> account.accountKey == accountKey }

    /** Forgets the sign-in in use; the next kept one is in use after it. */
    @Synchronized
    override fun delete() {
        val ring = accounts()
        val key = ring.active?.accountKey ?: return
        write(ring.remove(key))
    }

    /** Every sign-in, and the one in use. */
    @Synchronized
    fun accounts(): AccountRing {
        val stored = secrets.read(NAME) ?: return AccountRing()
        return TeaNodeJson.decodeFromString(AccountRing.serializer(), stored.toString(Charsets.UTF_8))
    }

    /** Keeps a sign-in and puts it to use. */
    @Synchronized
    fun add(account: Account) = write(accounts().put(account).activate(account.accountKey))

    @Synchronized
    fun activate(accountKey: String) = write(accounts().activate(accountKey))

    @Synchronized
    fun remove(accountKey: String) = write(accounts().remove(accountKey))

    private fun write(ring: AccountRing) {
        secrets.write(NAME, TeaNodeJson.encodeToString(AccountRing.serializer(), ring).toByteArray(Charsets.UTF_8))
    }

    private companion object {
        const val NAME = "accounts"
    }
}
