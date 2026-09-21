-- Templates and layouts in more than one language.
--
-- A template keeps one row and one name; its subject and content in other
-- locales are rows here, one per locale. The row's own content is the
-- default, and "locale" says which language that is, when the operator has
-- said. Sending names a locale and the closest translation is used, so a
-- caller's template name does not change with the language.
ALTER TABLE "template" ADD COLUMN "locale" character varying(32) NOT NULL DEFAULT '';
ALTER TABLE "layout" ADD COLUMN "locale" character varying(32) NOT NULL DEFAULT '';

CREATE TABLE "template_translation" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,

    -- Removing a template takes its translations with it.
    "template_id" character varying(32) NOT NULL REFERENCES "template" ("id") ON DELETE CASCADE,

    "locale" character varying(32) NOT NULL,
    "subject" character varying(256),
    "html_content" text,
    "text_content" text,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX idx_template_translation_template_id_locale ON "template_translation" USING btree (template_id, locale);

CREATE TABLE "layout_translation" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    "layout_id" character varying(32) NOT NULL REFERENCES "layout" ("id") ON DELETE CASCADE,
    "locale" character varying(32) NOT NULL,
    "html_content" text,
    "text_content" text,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX idx_layout_translation_layout_id_locale ON "layout_translation" USING btree (layout_id, locale);
