/*
The skill vocabulary, and being available.

Skills are rows a profile points at, never text a profile holds. "Go",
"golang" and "Golang" typed by three people are one skill here, and searching
for it finds all three — which is the whole reason for a controlled vocabulary
rather than a tag field.
*/
CREATE TABLE skills (
    id          UUID        PRIMARY KEY,
    name        TEXT        NOT NULL,
    -- Lower-cased and with runs of space collapsed, so "Node.js" and
    -- "node.js " are the same name. Unique across the vocabulary and the
    -- queue together: a proposal cannot shadow a skill that exists.
    name_folded TEXT        NOT NULL UNIQUE,
    description TEXT        NOT NULL DEFAULT '',
    /*
      'active' is in the vocabulary: attachable by anybody, and searchable.
      'proposed' is somebody asking for it to be: attachable only by the person
      who proposed it, shown only to them and to whoever reviews the queue, and
      never searchable. A rejected proposal is deleted rather than kept, so it
      leaves nothing behind on anybody's profile.
    */
    status      TEXT        NOT NULL DEFAULT 'proposed',
    proposed_by UUID        REFERENCES users(id) ON DELETE SET NULL,
    reviewed_by UUID        REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT skills_status_valid CHECK (status IN ('active', 'proposed')),
    CONSTRAINT skills_name_not_empty CHECK (length(trim(name)) > 0)
);

CREATE INDEX skills_queue_idx ON skills (created_at) WHERE status = 'proposed';

/*
Other spellings of a skill.

What stops fragmentation after the fact: when a proposal turns out to be a
skill that already exists under another name, a reviewer merges it, its name
becomes an alias, and anybody typing that spelling later lands on the one
entry.
*/
CREATE TABLE skill_aliases (
    alias_folded TEXT PRIMARY KEY,
    skill_id     UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE
);

/*
The skills on a profile.

The description is the person's claim about that skill — "six years, mostly
payment integrations" — and is what a moderator verifies when they verify a
skill, which is why updated_at moves when it changes and not when the skill is
merely reordered.
*/
CREATE TABLE profile_skills (
    user_id     UUID        NOT NULL REFERENCES directory_profiles(user_id) ON DELETE CASCADE,
    skill_id    UUID        NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    description TEXT        NOT NULL DEFAULT '',
    position    INTEGER     NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (user_id, skill_id)
);

-- Searching by skill starts from the skill.
CREATE INDEX profile_skills_skill_idx ON profile_skills (skill_id);

/*
Availability: a statement about work, not about presence.

Whether somebody is open to work, the kind of engagement they want, and until
when that stays true. Deliberately nothing to do with user_presence: somebody
can be online with their presence hidden and correctly listed as available, or
connected all day and not looking. The expiry is what keeps a profile
somebody abandoned from advertising them indefinitely — past it they are simply
not available, with nothing about the profile changed, until they say so again.
*/
ALTER TABLE directory_profiles
    ADD COLUMN open_to_work           BOOLEAN     NOT NULL DEFAULT FALSE,
    ADD COLUMN availability_note      TEXT        NOT NULL DEFAULT '',
    ADD COLUMN availability_expires_at TIMESTAMPTZ,
    ADD CONSTRAINT directory_profiles_availability_expires
        CHECK (NOT open_to_work OR availability_expires_at IS NOT NULL);

/*
The starting vocabulary. An operator adds to it through the review queue: a
skill proposed by somebody who may manage the vocabulary goes straight in.
*/
INSERT INTO skills (id, name, name_folded, status) VALUES
    (gen_random_uuid(), 'Go', 'go', 'active'),
    (gen_random_uuid(), 'Python', 'python', 'active'),
    (gen_random_uuid(), 'JavaScript', 'javascript', 'active'),
    (gen_random_uuid(), 'TypeScript', 'typescript', 'active'),
    (gen_random_uuid(), 'Java', 'java', 'active'),
    (gen_random_uuid(), 'Kotlin', 'kotlin', 'active'),
    (gen_random_uuid(), 'Swift', 'swift', 'active'),
    (gen_random_uuid(), 'Dart', 'dart', 'active'),
    (gen_random_uuid(), 'Flutter', 'flutter', 'active'),
    (gen_random_uuid(), 'PHP', 'php', 'active'),
    (gen_random_uuid(), 'Laravel', 'laravel', 'active'),
    (gen_random_uuid(), 'Ruby', 'ruby', 'active'),
    (gen_random_uuid(), 'Rust', 'rust', 'active'),
    (gen_random_uuid(), 'C#', 'c#', 'active'),
    (gen_random_uuid(), '.NET', '.net', 'active'),
    (gen_random_uuid(), 'C++', 'c++', 'active'),
    (gen_random_uuid(), 'React', 'react', 'active'),
    (gen_random_uuid(), 'React Native', 'react native', 'active'),
    (gen_random_uuid(), 'Vue', 'vue', 'active'),
    (gen_random_uuid(), 'Nuxt', 'nuxt', 'active'),
    (gen_random_uuid(), 'Angular', 'angular', 'active'),
    (gen_random_uuid(), 'Node.js', 'node.js', 'active'),
    (gen_random_uuid(), 'Django', 'django', 'active'),
    (gen_random_uuid(), 'Spring Boot', 'spring boot', 'active'),
    (gen_random_uuid(), 'PostgreSQL', 'postgresql', 'active'),
    (gen_random_uuid(), 'MySQL', 'mysql', 'active'),
    (gen_random_uuid(), 'MongoDB', 'mongodb', 'active'),
    (gen_random_uuid(), 'Docker', 'docker', 'active'),
    (gen_random_uuid(), 'Kubernetes', 'kubernetes', 'active'),
    (gen_random_uuid(), 'Linux administration', 'linux administration', 'active'),
    (gen_random_uuid(), 'AWS', 'aws', 'active'),
    (gen_random_uuid(), 'Google Cloud', 'google cloud', 'active'),
    (gen_random_uuid(), 'Azure', 'azure', 'active'),
    (gen_random_uuid(), 'Android', 'android', 'active'),
    (gen_random_uuid(), 'iOS', 'ios', 'active'),
    (gen_random_uuid(), 'UI design', 'ui design', 'active'),
    (gen_random_uuid(), 'UX research', 'ux research', 'active'),
    (gen_random_uuid(), 'Product management', 'product management', 'active'),
    (gen_random_uuid(), 'Data analysis', 'data analysis', 'active'),
    (gen_random_uuid(), 'Machine learning', 'machine learning', 'active'),
    (gen_random_uuid(), 'DevOps', 'devops', 'active'),
    (gen_random_uuid(), 'Security', 'security', 'active'),
    (gen_random_uuid(), 'Technical writing', 'technical writing', 'active'),
    (gen_random_uuid(), 'Mobile money integration', 'mobile money integration', 'active'),
    (gen_random_uuid(), 'USSD applications', 'ussd applications', 'active'),
    (gen_random_uuid(), 'WordPress', 'wordpress', 'active');

INSERT INTO skill_aliases (alias_folded, skill_id)
SELECT alias, id FROM skills
JOIN (VALUES
    ('golang', 'go'),
    ('js', 'javascript'),
    ('ecmascript', 'javascript'),
    ('ts', 'typescript'),
    ('postgres', 'postgresql'),
    ('psql', 'postgresql'),
    ('k8s', 'kubernetes'),
    ('reactjs', 'react'),
    ('react.js', 'react'),
    ('vue.js', 'vue'),
    ('vuejs', 'vue'),
    ('nodejs', 'node.js'),
    ('node', 'node.js'),
    ('csharp', 'c#'),
    ('dotnet', '.net'),
    ('cpp', 'c++'),
    ('gcp', 'google cloud'),
    ('amazon web services', 'aws'),
    ('m-pesa integration', 'mobile money integration'),
    ('mpesa integration', 'mobile money integration')
) AS spelling(alias, canonical) ON skills.name_folded = spelling.canonical;
