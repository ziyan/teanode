-- Visits stored before the server knew a pass-by from a visit. A pass-by is
-- the phone crossing a place's circle on the way somewhere else: a visit from
-- the circle (region) shorter than three minutes, with a position within two
-- minutes of it at 4 m/s or faster, and none during it at 0.5 m/s or slower.
-- The same rule as DeletePassByVisits, written out here because a migration
-- does not change when the code does.
DELETE FROM "place_visit" "visit"
WHERE "visit"."visit_source" = 'region'
  AND "visit"."arrived_at" IS NOT NULL AND "visit"."departed_at" IS NOT NULL
  AND "visit"."departed_at" - "visit"."arrived_at" < interval '3 minutes'
  AND EXISTS (SELECT 1 FROM "location_point" "point"
      WHERE "point"."user_id" = "visit"."user_id"
        AND "point"."measured_at" >= "visit"."arrived_at" - interval '2 minutes'
        AND "point"."measured_at" <= "visit"."departed_at" + interval '2 minutes'
        AND "point"."speed_meters_per_second" >= 4)
  AND NOT EXISTS (SELECT 1 FROM "location_point" "point"
      WHERE "point"."user_id" = "visit"."user_id"
        AND "point"."measured_at" >= "visit"."arrived_at" AND "point"."measured_at" <= "visit"."departed_at"
        AND "point"."speed_meters_per_second" <= 0.5);

-- A visit from iOS visit monitoring that ended the moment it began. iOS
-- reports a visit again with its departure and a more exact position, which
-- the server took for another visit; that one's arrival ended the first at
-- once. iOS only reports a visit after a stay, so none lasts no time at all.
DELETE FROM "place_visit"
WHERE "visit_source" = 'visit' AND "arrived_at" IS NOT NULL AND "departed_at" = "arrived_at";
