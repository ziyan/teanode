DROP TABLE "layout_translation";
DROP TABLE "template_translation";
ALTER TABLE "layout" DROP COLUMN "locale";
ALTER TABLE "template" DROP COLUMN "locale";
