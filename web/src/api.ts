// path: web/src/api.ts

import { RunRecord, ArtifactRecord, BillingBalance, TopUpResponse } from './types';

const API_BASE = import.meta.env.VITE_API_URL || '';

export async function fetchRuns(limit = 50, offset = 0): Promise<RunRecord[]> {
  const res = await fetch(`${API_BASE}/runs?limit=${limit}&offset=${offset}`);
  if (!res.ok) throw new Error(`Failed to fetch runs: ${res.statusText}`);
  return res.json();
}

export async function fetchRun(id: string): Promise<RunRecord> {
  const res = await fetch(`${API_BASE}/runs/${id}`);
  if (!res.ok) throw new Error(`Failed to fetch run ${id}: ${res.statusText}`);
  return res.json();
}

export async function triggerRun(data: {
  repo_url: string;
  branch: string;
  commit_sha?: string;
  pipeline_yaml?: string;
}): Promise<RunRecord> {
  const res = await fetch(`${API_BASE}/runs`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Failed to trigger run: ${res.statusText}`);
  }
  return res.json();
}

export async function cancelRun(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/runs/${id}/cancel`, { method: 'POST' });
  if (!res.ok) throw new Error(`Failed to cancel run: ${res.statusText}`);
}

export async function fetchArtifacts(runId: string): Promise<ArtifactRecord[]> {
  const res = await fetch(`${API_BASE}/runs/${runId}/artifacts`);
  if (!res.ok) throw new Error(`Failed to fetch artifacts: ${res.statusText}`);
  return res.json();
}

export async function getArtifactDownloadUrl(runId: string, artifactId: string): Promise<string> {
  const res = await fetch(`${API_BASE}/runs/${runId}/artifacts/${artifactId}/download`);
  if (!res.ok) throw new Error(`Failed to get artifact download link`);
  const data = await res.json();
  return data.download_url;
}

export async function fetchSecrets(): Promise<string[]> {
  const res = await fetch(`${API_BASE}/secrets`);
  if (!res.ok) throw new Error(`Failed to fetch secrets: ${res.statusText}`);
  return res.json();
}

export async function createSecret(name: string, value: string): Promise<void> {
  const res = await fetch(`${API_BASE}/secrets`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, value }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Failed to save secret`);
  }
}

export async function deleteSecret(name: string): Promise<void> {
  const res = await fetch(`${API_BASE}/secrets/${name}`, { method: 'DELETE' });
  if (!res.ok) throw new Error(`Failed to delete secret`);
}

export async function fetchBillingBalance(userId = 'default_user'): Promise<BillingBalance> {
  const res = await fetch(`${API_BASE}/billing/balance?user_id=${encodeURIComponent(userId)}`);
  if (!res.ok) throw new Error(`Failed to fetch billing balance`);
  return res.json();
}

export async function initiateMpesaTopup(
  phoneNumber: string,
  amountKes: number,
  userId = 'default_user'
): Promise<TopUpResponse> {
  const res = await fetch(`${API_BASE}/billing/mpesa/topup`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      phone_number: phoneNumber,
      amount_kes: amountKes,
      user_id: userId,
    }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `M-Pesa STK push initiation failed`);
  }
  return res.json();
}

