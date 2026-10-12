-- Device credentials belong to accounts and grant collection sync only.
CREATE TABLE "sync_credential" (
    "id" varchar(32) PRIMARY KEY,
    "user_id" varchar(32) NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
    "created_at" timestamptz NOT NULL,
    "credential_name" varchar(128) NOT NULL,
    "selector" varchar(32) NOT NULL UNIQUE,
    "password_hash" text NOT NULL,
    "has_calendar_access" boolean NOT NULL DEFAULT false,
    "has_reminders_access" boolean NOT NULL DEFAULT false,
    "has_contacts_access" boolean NOT NULL DEFAULT false,
    "last_used_at" timestamptz
);
CREATE INDEX "sync_credential_user_id" ON "sync_credential"("user_id");
