/*
The project an engagement's agreed work is tracked in.

A pointer and nothing more. What a project member sees through it is the
project's own issues, under the project's own permissions; the engagement's
proposals, prices and conversation stay behind the engagement's two parties,
because nothing that reads a project ever joins to an engagement's terms.

Set null when the project goes, so deleting a project never deletes the record
of an agreement.
*/
ALTER TABLE engagements ADD COLUMN project_id UUID REFERENCES projects(id) ON DELETE SET NULL;

CREATE INDEX engagements_project_idx ON engagements (project_id) WHERE project_id IS NOT NULL;
