ALTER TABLE directory_profiles
    DROP CONSTRAINT IF EXISTS directory_profiles_availability_expires,
    DROP COLUMN IF EXISTS availability_expires_at,
    DROP COLUMN IF EXISTS availability_note,
    DROP COLUMN IF EXISTS open_to_work;
DROP TABLE IF EXISTS profile_skills;
DROP TABLE IF EXISTS skill_aliases;
DROP TABLE IF EXISTS skills;
