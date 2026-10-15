-- What the daily check of a provider's prices against its bill last did, one
-- row per provider so a restart does not run the check again the same day and
-- the dashboard can say when it ran. compared_json lists each price it set
-- beside the bill (model, tokenKind, configuredPerMillion, billedPerMillion,
-- dayCount); changed_json lists the prices it then changed (model, tokenKind,
-- beforePerMillion, afterPerMillion). error_message is why the bill could not
-- be read, such as a refused key, and is empty when it could.
CREATE TABLE "agent_billing_check" (
    "provider_name" text PRIMARY KEY,
    "checked_at" timestamptz NOT NULL,
    "error_message" text NOT NULL DEFAULT '',
    "compared_json" jsonb NOT NULL DEFAULT '[]',
    "changed_json" jsonb NOT NULL DEFAULT '[]'
);
