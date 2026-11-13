package com.teanode.kit.mail

import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class MailPicturePathTest {
    @Test fun `a file of the same message is served`() {
        assertTrue(MailWords.isOwnMessagePath("/api/v1/mail/m1/remote?url=https%3A%2F%2Fexample.com%2Fa.png", "m1"))
        assertTrue(MailWords.isOwnMessagePath("/api/v1/mail/m1/attachment/a1", "m1"))
    }

    @Test fun `another message and other api paths are not`() {
        assertFalse(MailWords.isOwnMessagePath("/api/v1/mail/m2/remote?url=x", "m1"))
        assertFalse(MailWords.isOwnMessagePath("/api/v1/mailbox/drafts", "m1"))
        assertFalse(MailWords.isOwnMessagePath("/api/v1/account/sessions", "m1"))
        assertFalse(MailWords.isOwnMessagePath("/api/v1/mail/m1", "m1"))
        assertFalse(MailWords.isOwnMessagePath("/api/v1/mail/m10/remote", "m1"))
    }

    @Test fun `a path that climbs out of the message is not`() {
        assertFalse(MailWords.isOwnMessagePath("/api/v1/mail/m1/../../account/sessions", "m1"))
        assertFalse(MailWords.isOwnMessagePath("/api/v1/mail/m1/%2e%2e/%2e%2e/account", "m1"))
        assertFalse(MailWords.isOwnMessagePath("/api/v1/mail/m1/a%2fb", "m1"))
    }

    @Test fun `an empty message id serves nothing`() {
        assertFalse(MailWords.isOwnMessagePath("/api/v1/mail//remote", ""))
    }
}
