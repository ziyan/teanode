-- The units a person reads measurements in: temperatures, distances and
-- weights. Empty is automatic, which reads them from the person's language
-- and region as before; metric is degrees Celsius, kilometers and
-- kilograms; us is degrees Fahrenheit, miles and pounds.
--
-- It is a preference about how a number is shown, never about how it is
-- stored: every measurement the server keeps stays in metric units.
ALTER TABLE "user" ADD COLUMN "measurement_system" character varying(8) NOT NULL DEFAULT ''
    CONSTRAINT "user_measurement_system" CHECK ("measurement_system" IN ('', 'metric', 'us'));
