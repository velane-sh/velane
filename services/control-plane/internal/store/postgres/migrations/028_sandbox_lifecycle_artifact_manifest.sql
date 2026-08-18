-- Deployment details are distinct from the signed image-builder manifest
-- object identity. Ready artifacts without this immutable profile-specific
-- manifest remain intentionally undispatchable.
ALTER TABLE sandbox_image_recipe_profile_artifacts
  ADD COLUMN IF NOT EXISTS artifact_manifest JSONB NOT NULL DEFAULT '{}';

CREATE OR REPLACE FUNCTION sandbox_recipe_artifact_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.id IS DISTINCT FROM OLD.id
    OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
    OR NEW.recipe_version_id IS DISTINCT FROM OLD.recipe_version_id
    OR NEW.profile_version_id IS DISTINCT FROM OLD.profile_version_id
    OR (OLD.status='ready' AND (
      NEW.artifact_ref IS DISTINCT FROM OLD.artifact_ref
      OR NEW.artifact_digest IS DISTINCT FROM OLD.artifact_digest
      OR NEW.artifact_size IS DISTINCT FROM OLD.artifact_size
      OR NEW.artifact_manifest IS DISTINCT FROM OLD.artifact_manifest
      OR NEW.vm_restore_descriptor_digest IS DISTINCT FROM OLD.vm_restore_descriptor_digest
      OR NEW.status IS DISTINCT FROM OLD.status
    ))
  THEN
    RAISE EXCEPTION 'ready recipe artifact is immutable';
  END IF;
  RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS sandbox_recipe_artifact_immutable_trigger ON sandbox_image_recipe_profile_artifacts;
CREATE TRIGGER sandbox_recipe_artifact_immutable_trigger
  BEFORE UPDATE ON sandbox_image_recipe_profile_artifacts
  FOR EACH ROW EXECUTE FUNCTION sandbox_recipe_artifact_immutable();
