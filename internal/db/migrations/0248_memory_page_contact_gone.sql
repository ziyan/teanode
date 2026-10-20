-- A memory page names the contact it is about by the contact's ID alone, with
-- no foreign key, and nothing took the name off when the contact was deleted.
-- An address book read in again gives every contact a new ID, so a page could
-- go on naming a contact that is not there: the person card then found no
-- memory for that person, and the dreams, which leave a page that names a
-- contact alone, never tied it to the new one.
--
-- Deleting a contact or an address book now unties what named it. This takes
-- care of the ties made before that.
--
-- First, a page naming a contact that is gone is tied to the contact with
-- exactly its name, when that is one contact in the address books the agent
-- was given, no other page of the agent has that name, and no other page of
-- the agent names that contact already.
WITH "gone" AS (
    SELECT "agent_node"."id", "agent_node"."agent_id", "agent_node"."name", "agent"."user_id"
    FROM "agent_node" JOIN "agent" ON "agent"."id" = "agent_node"."agent_id"
    WHERE "agent_node"."contact_id" IS NOT NULL AND "agent_node"."kind" = 'person'
      AND NOT EXISTS (SELECT 1 FROM "contact" JOIN "addressbook" ON "addressbook"."id" = "contact"."addressbook_id"
          WHERE "addressbook"."user_id" = "agent"."user_id" AND "contact"."id" = "agent_node"."contact_id")
), "named" AS (
    SELECT "gone"."id", "gone"."agent_id", min("contact"."id") AS "contact_id"
    FROM "gone"
    JOIN "addressbook" ON "addressbook"."user_id" = "gone"."user_id" AND "addressbook"."agent_granted"
    JOIN "contact" ON "contact"."addressbook_id" = "addressbook"."id" AND "contact"."name" = "gone"."name"
    WHERE NOT EXISTS (SELECT 1 FROM "agent_node" AS "other"
        WHERE "other"."agent_id" = "gone"."agent_id" AND "other"."id" <> "gone"."id"
          AND "other"."kind" = 'person' AND "other"."name" = "gone"."name")
    GROUP BY "gone"."id", "gone"."agent_id"
    HAVING count(*) = 1
)
UPDATE "agent_node" SET "contact_id" = "named"."contact_id"
FROM "named"
WHERE "agent_node"."id" = "named"."id"
  AND NOT EXISTS (SELECT 1 FROM "agent_node" AS "other"
      WHERE "other"."agent_id" = "named"."agent_id" AND "other"."contact_id" = "named"."contact_id");

-- Then the rest of the ties to a contact that is gone are taken off: the
-- page's name, and the link that said the page is that contact.
UPDATE "agent_node" SET "contact_id" = NULL
FROM "agent"
WHERE "agent"."id" = "agent_node"."agent_id" AND "agent_node"."contact_id" IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM "contact" JOIN "addressbook" ON "addressbook"."id" = "contact"."addressbook_id"
      WHERE "addressbook"."user_id" = "agent"."user_id" AND "contact"."id" = "agent_node"."contact_id");

DELETE FROM "agent_record_link"
USING "agent"
WHERE "agent"."id" = "agent_record_link"."agent_id"
  AND "agent_record_link"."record_kind" = 'contact' AND "agent_record_link"."record_relation" = 'is_contact'
  AND NOT EXISTS (SELECT 1 FROM "contact" JOIN "addressbook" ON "addressbook"."id" = "contact"."addressbook_id"
      WHERE "addressbook"."user_id" = "agent"."user_id" AND "contact"."id" = "agent_record_link"."record_id");

-- And an account naming as its own a contact that is gone names none.
UPDATE "user" SET "contact_id" = NULL
WHERE "contact_id" IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM "contact" JOIN "addressbook" ON "addressbook"."id" = "contact"."addressbook_id"
      WHERE "addressbook"."user_id" = "user"."id" AND "contact"."id" = "user"."contact_id");
