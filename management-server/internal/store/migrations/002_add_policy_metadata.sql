-- Add policy_type column (distinguishes opa_rego from gnn_policy)
ALTER TABLE policy_bundles ADD COLUMN policy_type TEXT NOT NULL DEFAULT 'opa_rego';

-- Add metadata_json column for ML model params (svdd_center, r_max, thresholds, etc.)
ALTER TABLE policy_bundles ADD COLUMN metadata_json TEXT NOT NULL DEFAULT '{}';
