// path: web/src/types.ts

export type StepStatus = 'pending' | 'running' | 'success' | 'failure' | 'skipped' | 'cancelled';
export type RunStatus = 'pending' | 'running' | 'success' | 'failure' | 'error' | 'skipped' | 'cancelled';

export interface StepRecord {
  id: string;
  run_id: string;
  step_id: string;
  name: string;
  image: string;
  command: string;
  status: StepStatus;
  exit_code?: number;
  started_at?: string;
  completed_at?: string;
  duration_ms: number;
  created_at: string;
  depends_on?: string[];
}

export interface RunRecord {
  id: string;
  repo_url: string;
  commit_sha: string;
  branch: string;
  status: RunStatus;
  trigger: string;
  error_message?: string;
  created_at: string;
  started_at?: string;
  completed_at?: string;
  duration_ms: number;
  steps?: StepRecord[];
}

export interface ArtifactRecord {
  id: string;
  run_id: string;
  step_id: string;
  file_path: string;
  s3_key: string;
  size_bytes: number;
  created_at: string;
}

export interface SecretItem {
  name: string;
  value?: string;
}

export interface BillingBalance {
  user_id: string;
  balance_kes: number;
  currency: string;
}

export interface TopUpResponse {
  status: string;
  checkout_request_id: string;
  merchant_request_id: string;
  customer_message: string;
  amount_kes: number;
}

export interface LogLine {
  id?: number;
  stream: 'stdout' | 'stderr' | 'info';
  text: string;
  timestamp: string;
  step_id?: string;
}

