// path: web/src/components/SecretsView.tsx

import React, { useEffect, useState } from 'react';
import { KeyRound, Plus, Trash2, ShieldCheck, Eye, EyeOff, Loader2 } from 'lucide-react';
import { fetchSecrets, createSecret, deleteSecret } from '../api';

export const SecretsView: React.FC = () => {
  const [secrets, setSecrets] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // New secret form state
  const [name, setName] = useState('');
  const [value, setValue] = useState('');
  const [showValue, setShowValue] = useState(false);
  const [saving, setSaving] = useState(false);
  const [isModalOpen, setIsModalOpen] = useState(false);

  const loadSecrets = async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await fetchSecrets();
      setSecrets(data);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to fetch secrets');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadSecrets();
  }, []);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || !value.trim()) return;

    try {
      setSaving(true);
      setError(null);
      await createSecret(name.trim(), value.trim());
      setName('');
      setValue('');
      setIsModalOpen(false);
      await loadSecrets();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to save secret');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (secretName: string) => {
    if (!confirm(`Are you sure you want to delete secret "${secretName}"?`)) return;
    try {
      await deleteSecret(secretName);
      await loadSecrets();
    } catch (err: unknown) {
      alert(err instanceof Error ? err.message : 'Failed to delete secret');
    }
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-xl font-bold text-slate-100 flex items-center gap-2">
            <KeyRound className="w-5 h-5 text-blue-400" />
            Encrypted Secrets Vault
          </h2>
          <p className="text-sm text-slate-400 mt-1">
            Secrets are encrypted at rest with AES-256-GCM and masked automatically in log outputs.
          </p>
        </div>

        <button
          onClick={() => setIsModalOpen(true)}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg font-medium text-sm transition-colors shadow-lg shadow-blue-600/20"
        >
          <Plus className="w-4 h-4" />
          Add Secret
        </button>
      </div>

      {error && (
        <div className="p-4 bg-rose-950/40 border border-rose-800 rounded-lg text-sm text-rose-300">
          {error}
        </div>
      )}

      {/* Secrets Table */}
      <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm">
        {loading ? (
          <div className="flex items-center justify-center p-12 text-slate-400">
            <Loader2 className="w-6 h-6 animate-spin mr-2 text-blue-400" />
            Loading encrypted secrets...
          </div>
        ) : secrets.length === 0 ? (
          <div className="flex flex-col items-center justify-center p-12 text-slate-500">
            <KeyRound className="w-10 h-10 mb-3 opacity-40" />
            <p className="text-base font-medium">No secrets configured</p>
            <p className="text-xs text-slate-600 mt-1">
              Add environment secrets such as deploy tokens, API credentials, or certificates.
            </p>
          </div>
        ) : (
          <table className="w-full text-left text-sm text-slate-300">
            <thead className="bg-slate-950/60 text-xs uppercase text-slate-400 font-semibold border-b border-slate-800">
              <tr>
                <th className="px-6 py-3">Secret Key</th>
                <th className="px-6 py-3">Value</th>
                <th className="px-6 py-3">Encryption</th>
                <th className="px-6 py-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800/60">
              {secrets.map((secName) => (
                <tr key={secName} className="hover:bg-slate-850/40 transition-colors">
                  <td className="px-6 py-4 font-mono font-medium text-slate-200">
                    {secName}
                  </td>
                  <td className="px-6 py-4 font-mono text-slate-500">
                    ••••••••••••••••
                  </td>
                  <td className="px-6 py-4">
                    <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-emerald-950/50 text-emerald-400 border border-emerald-800/40">
                      <ShieldCheck className="w-3.5 h-3.5" />
                      AES-256-GCM
                    </span>
                  </td>
                  <td className="px-6 py-4 text-right">
                    <button
                      onClick={() => handleDelete(secName)}
                      className="p-1.5 text-slate-500 hover:text-rose-400 rounded-md transition-colors"
                      title="Delete secret"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Add Secret Modal */}
      {isModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/70 backdrop-blur-sm">
          <div className="w-full max-w-md bg-slate-900 border border-slate-800 rounded-2xl shadow-2xl overflow-hidden p-6">
            <h3 className="text-lg font-bold text-slate-100 mb-4 flex items-center gap-2">
              <KeyRound className="w-5 h-5 text-blue-400" />
              Store New Encrypted Secret
            </h3>

            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="block text-xs font-semibold text-slate-300 uppercase mb-1">
                  Secret Identifier (Uppercase / Underscores)
                </label>
                <input
                  type="text"
                  placeholder="e.g. MPESA_CONSUMER_SECRET"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  required
                  className="w-full px-3 py-2 bg-slate-950 border border-slate-800 rounded-lg text-sm text-slate-100 font-mono focus:outline-none focus:border-blue-500"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-300 uppercase mb-1">
                  Secret Plaintext Value
                </label>
                <div className="relative">
                  <input
                    type={showValue ? 'text' : 'password'}
                    placeholder="Enter confidential token or key"
                    value={value}
                    onChange={(e) => setValue(e.target.value)}
                    required
                    className="w-full pl-3 pr-10 py-2 bg-slate-950 border border-slate-800 rounded-lg text-sm text-slate-100 font-mono focus:outline-none focus:border-blue-500"
                  />
                  <button
                    type="button"
                    onClick={() => setShowValue(!showValue)}
                    className="absolute right-3 top-2.5 text-slate-500 hover:text-slate-300"
                  >
                    {showValue ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                  </button>
                </div>
              </div>

              <div className="flex items-center justify-end gap-3 pt-4 border-t border-slate-800">
                <button
                  type="button"
                  onClick={() => setIsModalOpen(false)}
                  className="px-4 py-2 rounded-lg text-sm font-medium text-slate-400 hover:text-slate-200 transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={saving}
                  className="flex items-center gap-2 px-5 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium transition-colors disabled:opacity-50"
                >
                  {saving && <Loader2 className="w-4 h-4 animate-spin" />}
                  Encrypt & Save
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};

