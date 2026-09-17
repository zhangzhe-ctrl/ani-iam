-- First-administrator and invitation account creation may insert a verified
-- email only after the independent identity proof. Existing ownership cannot
-- be overwritten by the runtime role.
GRANT INSERT ON verified_emails TO ani_iam_runtime;
UPDATE iam_schema_revision SET revision='202609130003' WHERE singleton;
