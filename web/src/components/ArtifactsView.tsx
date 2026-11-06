// path: web/src/components/ArtifactsView.tsx

import React, { useEffect, useState } from 'react';
import { Package, Download, ExternalLink, Loader2, FolderArchive } from 'lucide-react';
import { ArtifactRecord, RunRecord } from '../types';
import { fetchArtifacts, fetchRuns, getArtifactDownloadUrl } from '../api';

export const ArtifactsView: React.FC = () => {
  const [runs, setRuns] = useState<RunRecord[]>([]);
  const [selectedRunId, setSelectedRunId] = useState<string>('');
  const [artifacts, setArtifacts] = useState<ArtifactRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [downloadingId, setDownloadingId] = useState<string | null>(null);

  useEffect(() => {
    fetchRuns(20, 0)
      .then((data) => {
        setRuns(data);
        if (data.length > 0) {
          setSelectedRunId(data[0].id);
        }
      })
      .catch((err) => console.error('Failed to load runs for artifacts:', err));
  }, []);

  useEffect(() => {
    if (!selectedRunId) return;
    setLoading(true);
    fetchArtifacts(selectedRunId)
      .then((data) => setArtifacts(data))
      .catch((err) => console.error('Failed to fetch artifacts:', err))
      .finally(() => setLoading(false));
  }, [selectedRunId]);

  const handleDownload = async (artifactId: string) => {
    try {
      setDownloadingId(artifactId);
      const url = await getArtifactDownloadUrl(selectedRunId, artifactId);
      window.open(url, '_blank');
    } catch (err: unknown) {
      alert(err instanceof Error ? err.message : 'Failed to download artifact');
    } finally {
      setDownloadingId(null);
    }
  };

  return (
    <div className="space-y-6">
      {/* Header & Run Selector */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
        <div>
          <h2 className="text-xl font-bold text-slate-100 flex items-center gap-2">
            <Package className="w-5 h-5 text-blue-400" />
            Build Artifacts Repository
          </h2>
          <p className="text-sm text-slate-400 mt-1">
            MinIO S3-compatible storage retaining compiled binaries, logs, and coverage reports.
          </p>
        </div>

        {runs.length > 0 && (
          <div className="flex items-center gap-2">
            <span className="text-xs text-slate-400">Select Run:</span>
            <select
              value={selectedRunId}
              onChange={(e) => setSelectedRunId(e.target.value)}
              className="px-3 py-1.5 bg-slate-900 border border-slate-800 rounded-lg text-sm text-slate-200 font-mono focus:outline-none focus:border-blue-500"
            >
              {runs.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.id.slice(0, 8)} — {r.branch} ({r.status})
                </option>
              ))}
            </select>
          </div>
        )}
      </div>

      {/* Artifacts Table */}
      <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm">
        {loading ? (
          <div className="flex items-center justify-center p-12 text-slate-400">
            <Loader2 className="w-6 h-6 animate-spin mr-2 text-blue-400" />
            Fetching stored artifacts...
          </div>
        ) : artifacts.length === 0 ? (
          <div className="flex flex-col items-center justify-center p-12 text-slate-500">
            <FolderArchive className="w-10 h-10 mb-3 opacity-40" />
            <p className="text-base font-medium">No artifacts captured for this run</p>
            <p className="text-xs text-slate-600 mt-1">
              Configure `artifacts` patterns (e.g. `dist/**`) in your pipeline YAML definition.
            </p>
          </div>
        ) : (
          <table className="w-full text-left text-sm text-slate-300">
            <thead className="bg-slate-950/60 text-xs uppercase text-slate-400 font-semibold border-b border-slate-800">
              <tr>
                <th className="px-6 py-3">File Path</th>
                <th className="px-6 py-3">Step</th>
                <th className="px-6 py-3">Size</th>
                <th className="px-6 py-3">S3 Storage Key</th>
                <th className="px-6 py-3 text-right">Download</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800/60">
              {artifacts.map((art) => (
                <tr key={art.id} className="hover:bg-slate-850/40 transition-colors">
                  <td className="px-6 py-4 font-mono font-medium text-slate-200 flex items-center gap-2">
                    <Download className="w-4 h-4 text-slate-500" />
                    {art.file_path}
                  </td>
                  <td className="px-6 py-4">
                    <span className="px-2 py-0.5 rounded bg-slate-800 border border-slate-700 text-xs font-mono text-slate-300">
                      {art.step_id}
                    </span>
                  </td>
                  <td className="px-6 py-4 font-mono text-xs text-slate-400">
                    {formatBytes(art.size_bytes)}
                  </td>
                  <td className="px-6 py-4 font-mono text-xs text-slate-500 truncate max-w-xs">
                    {art.s3_key}
                  </td>
                  <td className="px-6 py-4 text-right">
                    <button
                      onClick={() => handleDownload(art.id)}
                      disabled={downloadingId === art.id}
                      className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-slate-800 hover:bg-slate-700 border border-slate-700 text-xs text-slate-200 font-medium transition-colors"
                    >
                      {downloadingId === art.id ? (
                        <Loader2 className="w-3.5 h-3.5 animate-spin" />
                      ) : (
                        <ExternalLink className="w-3.5 h-3.5" />
                      )}
                      Presigned URL
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
};

function formatBytes(bytes: number): string {
  if (bytes <= 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
}

