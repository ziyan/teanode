-- The whole schema. Only data that grows without bound lives here:
-- received mail, delivery attempts, DMARC reports, usage counters, and the
-- templates the mailer renders. Domains, aliases, credentials and dashboard
-- users are configuration and live in teanode.yaml.
--
-- domain_id, alias_id and credential_id hold identifiers from that file. They
-- are deliberately not foreign keys: the configuration entry they name can be
-- deleted while the row lives on, and reading code treats a missing entry as
-- "deleted" rather than as corruption.

CREATE TABLE "mail" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    "domain_id" character varying(32),
    "credential_id" character varying(32),
    "delivery_id" character varying(32),
    "envelope_id" character varying(32),
    "hello" character varying(256),
    "ip" character varying(64),
    "rdns" character varying(256),
    "tls_version" character varying(64),
    "tls_cipher_suite" character varying(64),
    "location" jsonb,
    "sender" character varying(320),
    "recipients" character varying(320)[],
    "message_id" character varying(320),
    "from" character varying(320),
    "subject" text,
    "size" bigint,
    "status" character varying(32),
    "authentication_results" jsonb,
    "received_at" timestamp with time zone,
    "kind" character varying(32),
    PRIMARY KEY ("id")
);

-- The dashboard lists mail newest first, filtered by domain. Identifiers are
-- ULIDs, which sort by creation time, so the primary key already orders by
-- age and only the domain filter needs help.
CREATE INDEX idx_mail_domain_id ON "mail" USING btree (domain_id);
CREATE INDEX idx_mail_received_at ON "mail" USING btree (received_at);

CREATE TABLE "delivery" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    "mail_id" character varying(32),
    "alias_id" character varying(32),
    "recipient" character varying(320),
    "kind" character varying(32),
    "status" character varying(32),
    "size" bigint,
    "attempted_at" timestamp with time zone,
    "delivered_at" timestamp with time zone,
    "dropped_at" timestamp with time zone,
    "notified_at" timestamp with time zone,
    "retry_at" timestamp with time zone,
    "attempts" bigint,
    "error" text,
    "delivery_statuses" jsonb,
    PRIMARY KEY ("id")
);
ALTER TABLE "delivery" ADD CONSTRAINT delivery_mail_id_mail_id_foreign FOREIGN KEY (mail_id) REFERENCES "mail"(id) ON DELETE CASCADE ON UPDATE RESTRICT;
CREATE INDEX idx_delivery_mail_id ON "delivery" USING btree (mail_id);
CREATE INDEX idx_delivery_alias_id ON "delivery" USING btree (alias_id);

-- The retry loop asks for deliveries due to be attempted again, once a
-- minute, forever. Without this index that query scans the whole table.
CREATE INDEX idx_delivery_retry_at ON "delivery" USING btree (retry_at) WHERE retry_at IS NOT NULL;

CREATE TABLE "report" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    "mail_id" character varying(32),
    "domain_id" character varying(32),
    "begin_at" timestamp with time zone,
    "end_at" timestamp with time zone,
    "count" bigint,
    "ip" character varying(64),
    "rdns" character varying(256),
    "location" jsonb,
    "from_domain" character varying(256),
    "sender_domain" character varying(256),
    "disposition" character varying(32),
    "dkim_aligned" boolean,
    "spf_aligned" boolean,
    "feedback" jsonb,
    PRIMARY KEY ("id")
);
ALTER TABLE "report" ADD CONSTRAINT report_mail_id_mail_id_foreign FOREIGN KEY (mail_id) REFERENCES "mail"(id) ON DELETE CASCADE ON UPDATE RESTRICT;
CREATE INDEX idx_report_domain_id ON "report" USING btree (domain_id);

-- Usage counters, aggregated in memory and flushed periodically. backend_id
-- names the instance that recorded them, so several instances writing to one
-- database do not overwrite each other.
CREATE TABLE "domain_usage" (
    "backend_id" character varying(32) NOT NULL,
    "domain_id" character varying(32) NOT NULL,
    "timestamp" bigint,
    "interval" bigint,
    "values" bigint[],
    PRIMARY KEY ("backend_id", "domain_id", "timestamp", "interval")
);

CREATE TABLE "alias_usage" (
    "backend_id" character varying(32) NOT NULL,
    "alias_id" character varying(32) NOT NULL,
    "timestamp" bigint,
    "interval" bigint,
    "values" bigint[],
    PRIMARY KEY ("backend_id", "alias_id", "timestamp", "interval")
);

CREATE TABLE "credential_usage" (
    "backend_id" character varying(32) NOT NULL,
    "credential_id" character varying(32) NOT NULL,
    "timestamp" bigint,
    "interval" bigint,
    "values" bigint[],
    PRIMARY KEY ("backend_id", "credential_id", "timestamp", "interval")
);

CREATE TABLE "layout" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    "domain_id" character varying(32),
    "comment" text,
    "html_content" text,
    "text_content" text,
    PRIMARY KEY ("id")
);
CREATE INDEX idx_layout_domain_id ON "layout" USING btree (domain_id);

CREATE TABLE "template" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    "domain_id" character varying(32),
    "layout_id" character varying(32),
    "name" character varying(32) NOT NULL,
    "comment" text,
    "subject" character varying(256),
    "html_content" text,
    "text_content" text,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX idx_template_domain_id_name ON "template" USING btree (domain_id, name);
ALTER TABLE "template" ADD CONSTRAINT template_layout_id_layout_id_foreign FOREIGN KEY (layout_id) REFERENCES "layout"(id) ON DELETE SET NULL ON UPDATE RESTRICT;
