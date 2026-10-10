-- Pairing codes are minutes old at most; nothing waiting on one survives a
-- downgrade, and a device asks for a new code.
DROP TABLE "oauth_device_authorization";
