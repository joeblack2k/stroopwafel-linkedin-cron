ALTER TABLE posts ADD COLUMN planning_approved INTEGER NOT NULL DEFAULT 0;

UPDATE posts
SET planning_approved = 1
WHERE approval_pending = 0
  AND status IN ('scheduled', 'sent', 'failed');
