ALTER TABLE directory_profiles ADD COLUMN location TEXT NOT NULL DEFAULT '';

-- Back onto the profiles that exist; a location on an account with no
-- profile has nowhere to go and is dropped with the table.
UPDATE directory_profiles p SET location = l.place
FROM user_locations l WHERE l.user_id = p.user_id;

DROP FUNCTION person_location(UUID, SMALLINT, SMALLINT);
DROP TABLE user_locations;
