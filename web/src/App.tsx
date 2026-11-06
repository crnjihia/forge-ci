// path: web/src/App.tsx

import React, { useEffect, useState } from 'react';
import {
  Hammer,
  Play,
  Layers,
  Terminal,
  KeyRound,
  Package,
  Smartphone,
  CheckCircle2,
  XCircle,
  Loader2,
  Clock,
  MinusCircle,
  GitBranch,
  RefreshCw,
  X,
} from 'lucide-react';
import { RunRecord, RunStatus } from './types';
import { fetchRuns, fetchRun, cancelRun, fetchBillingBalance } from './api';
import { DAGView } from './components/DAGView';
import { LogViewer } from './components/LogViewer';
import { SecretsView } from './components/SecretsView';
import { ArtifactsView } from './components/ArtifactsView';
import { BillingView } from './components/BillingView';
import { TriggerRunModal } from './components/TriggerRunModal';

type TabType = 'runs' | 'dag_logs' | 'secrets' | 'artifacts' | 'billing';

export const App: React.FC = () => {
  const [activeTab, setActiveTab] = useState<TabType>('runs');
  const [runs, setRuns] = useState<RunRecord[]>([]);
  const [selectedRun, setSelectedRun] = useState<RunRecord | null>(null);
  const [selectedStepId, setSelectedStepId] = useState<string | null>(null);
  const [balanceKes, setBalanceKes] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [isTriggerModalOpen, setIsTriggerModalOpen] = useState(false);
  const [toastMessage, setToastMessage] = useState<string | null>(null);

  // Load runs
  const loadRuns = async () => {
    try {
      const data = await fetchRuns(50, 0);
      setRuns(data);
      if (!selectedRun && data.length > 0) {
        setSelectedRun(data[0]);
      } else if (selectedRun) {
        // Refresh selected run details
        const updated = data.find((r) => r.id === selectedRun.id);
        if (updated) {
          setSelectedRun(updated);
        }
      }
    } catch (err) {
      console.error('Error fetching runs:', err);
    } finally {
      setLoading(false);
    }
  };

  // Load balance
  const loadBalance = async () => {
    try {
      const bal = await fetchBillingBalance('default_user');
      setBalanceKes(bal.balance_kes);
    } catch {
      // Ignored in offline mock
    }
  };

  useEffect(() => {
    loadRuns();
    loadBalance();

    const interval = setInterval(() => {
      loadRuns();
      loadBalance();
    }, 4000);

    return () => clearInterval(interval);
  }, [selectedRun?.id]);

  const handleSelectRun = async (run: RunRecord) => {
    setSelectedRun(run);
    setSelectedStepId(null);
    try {
      const full = await fetchRun(run.id);
      setSelectedRun(full);
    } catch (err) {
      console.error('Error fetching run details:', err);
    }
  };

  const handleRunTriggered = (newRun: RunRecord) => {
    setRuns((prev) => [newRun, ...prev]);
    setSelectedRun(newRun);
    setActiveTab('dag_logs');
    showToast(`Run initiated! Monitoring commit ${newRun.commit_sha.slice(0, 7)}...`);
  };

  const handleCancelRun = async (runId: string) => {
    if (!confirm('Are you sure you want to cancel this pipeline execution?')) return;
    try {
      await cancelRun(runId);
      loadRuns();
      showToast('Run cancellation signal sent.');
    } catch (err: unknown) {
      alert(err instanceof Error ? err.message : 'Failed to cancel run');
    }
  };

  const showToast = (msg: string) => {
    setToastMessage(msg);
    setTimeout(() => setToastMessage(null), 4000);
  };

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col font-sans selection:bg-blue-600 selection:text-white">
      {/* Top Navbar */}
      <header className="h-16 border-b border-slate-800 bg-slate-900/80 backdrop-blur-md sticky top-0 z-40 px-6 flex items-center justify-between">
        <div className="flex items-center gap-6">
          <div className="flex items-center gap-2.5">
            <div className="w-9 h-9 rounded-xl bg-blue-600 flex items-center justify-center text-white shadow-lg shadow-blue-600/30">
              <Hammer className="w-5 h-5" />
            </div>
            <div>
              <div className="font-extrabold text-base tracking-tight text-white flex items-center gap-2">
                Forge CI
                <span className="text-[10px] font-mono px-2 py-0.5 rounded-full bg-slate-800 text-slate-400 border border-slate-700">
                  v1.0.0
                </span>
              </div>
              <div className="text-[11px] text-slate-400 font-mono">
                Chris • Nairobi, Kenya (EAT)
              </div>
            </div>
          </div>

          {/* Navigation Tabs */}
          <nav className="hidden md:flex items-center gap-1 border-l border-slate-800 pl-6">
            <TabButton
              active={activeTab === 'runs'}
              onClick={() => setActiveTab('runs')}
              icon={<Layers className="w-4 h-4" />}
              label="Runs"
            />
            <TabButton
              active={activeTab === 'dag_logs'}
              onClick={() => setActiveTab('dag_logs')}
              icon={<Terminal className="w-4 h-4" />}
              label="DAG & Live Logs"
              badge={selectedRun ? selectedRun.status : undefined}
            />
            <TabButton
              active={activeTab === 'secrets'}
              onClick={() => setActiveTab('secrets')}
              icon={<KeyRound className="w-4 h-4" />}
              label="Secrets"
            />
            <TabButton
              active={activeTab === 'artifacts'}
              onClick={() => setActiveTab('artifacts')}
              icon={<Package className="w-4 h-4" />}
              label="Artifacts"
            />
            <TabButton
              active={activeTab === 'billing'}
              onClick={() => setActiveTab('billing')}
              icon={<Smartphone className="w-4 h-4" />}
              label="M-Pesa Billing"
            />
          </nav>
        </div>

        {/* Right Section: Balance & Trigger Button */}
        <div className="flex items-center gap-3">
          <button
            onClick={() => setActiveTab('billing')}
            className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-slate-850 hover:bg-slate-800 border border-slate-700/80 text-xs font-mono transition-colors"
          >
            <span className="text-emerald-400 font-bold">
              KES {balanceKes !== null ? balanceKes.toFixed(2) : '1,000.00'}
            </span>
            <span className="text-[10px] text-slate-400 uppercase font-sans">Credit</span>
          </button>

          <button
            onClick={() => setIsTriggerModalOpen(true)}
            className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg font-medium text-xs transition-colors shadow-lg shadow-blue-600/20"
          >
            <Play className="w-3.5 h-3.5 fill-current" />
            Trigger Run
          </button>
        </div>
      </header>

      {/* Main Content Area */}
      <main className="flex-1 max-w-7xl w-full mx-auto p-6">
        {activeTab === 'runs' && (
          <RunsTable
            runs={runs}
            loading={loading}
            onSelectRun={(r) => {
              handleSelectRun(r);
              setActiveTab('dag_logs');
            }}
            onCancelRun={handleCancelRun}
            onRefresh={loadRuns}
          />
        )}

        {activeTab === 'dag_logs' && (
          <div className="space-y-6">
            {/* Run Context Bar */}
            {selectedRun ? (
              <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 p-4 rounded-xl bg-slate-900 border border-slate-800">
                <div className="flex items-center gap-3">
                  {getStatusBadge(selectedRun.status)}
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="font-bold text-slate-100 font-mono text-sm">
                        Run #{selectedRun.id.slice(0, 8)}
                      </span>
                      <span className="text-slate-500">•</span>
                      <span className="flex items-center gap-1 text-xs text-slate-400">
                        <GitBranch className="w-3.5 h-3.5 text-blue-400" />
                        {selectedRun.branch}
                      </span>
                      <span className="text-slate-500">•</span>
                      <span className="font-mono text-xs text-slate-400">
                        {selectedRun.commit_sha.slice(0, 7)}
                      </span>
                    </div>
                    <div className="text-xs text-slate-500 truncate max-w-md mt-0.5">
                      {selectedRun.repo_url}
                    </div>
                  </div>
                </div>

                <div className="flex items-center gap-3">
                  <span className="text-xs font-mono text-slate-400">
                    Duration: {formatMs(selectedRun.duration_ms)}
                  </span>
                  {selectedRun.status === 'running' && (
                    <button
                      onClick={() => handleCancelRun(selectedRun.id)}
                      className="px-3 py-1 bg-rose-950/40 hover:bg-rose-900/60 text-rose-300 border border-rose-800 rounded-lg text-xs font-medium transition-colors"
                    >
                      Cancel Run
                    </button>
                  )}
                </div>
              </div>
            ) : (
              <div className="p-8 text-center text-slate-500 border border-dashed border-slate-800 rounded-xl">
                Select a run from the Runs tab to view execution details.
              </div>
            )}

            {/* Interactive DAG */}
            {selectedRun && (
              <DAGView
                steps={selectedRun.steps || []}
                selectedStepId={selectedStepId}
                onSelectStep={(stepId) => setSelectedStepId(stepId)}
              />
            )}

            {/* Live SSE Terminal Log Streamer */}
            {selectedRun && (
              <LogViewer runId={selectedRun.id} stepId={selectedStepId} />
            )}
          </div>
        )}

        {activeTab === 'secrets' && <SecretsView />}
        {activeTab === 'artifacts' && <ArtifactsView />}
        {activeTab === 'billing' && <BillingView />}
      </main>

      {/* Trigger Modal */}
      <TriggerRunModal
        isOpen={isTriggerModalOpen}
        onClose={() => setIsTriggerModalOpen(false)}
        onRunTriggered={handleRunTriggered}
      />

      {/* Bilingual Toast Alert */}
      {toastMessage && (
        <div className="fixed bottom-6 right-6 z-50 bg-slate-900 border border-slate-800 text-slate-100 text-xs px-4 py-3 rounded-xl shadow-2xl flex items-center gap-3 animate-fade-in">
          <span>{toastMessage}</span>
          <button onClick={() => setToastMessage(null)} className="text-slate-500 hover:text-slate-300">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}
    </div>
  );
};

interface TabButtonProps {
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  label: string;
  badge?: RunStatus;
}

const TabButton: React.FC<TabButtonProps> = ({ active, onClick, icon, label, badge }) => (
  <button
    onClick={onClick}
    className={`flex items-center gap-2 px-3.5 py-2 rounded-lg text-xs font-medium transition-colors ${
      active
        ? 'bg-slate-800 text-white shadow-sm'
        : 'text-slate-400 hover:text-slate-200 hover:bg-slate-850'
    }`}
  >
    {icon}
    <span>{label}</span>
    {badge && (
      <span
        className={`w-2 h-2 rounded-full ${
          badge === 'success'
            ? 'bg-emerald-400'
            : badge === 'failure'
            ? 'bg-rose-400'
            : badge === 'running'
            ? 'bg-blue-400 animate-pulse'
            : 'bg-slate-500'
        }`}
      />
    )}
  </button>
);

interface RunsTableProps {
  runs: RunRecord[];
  loading: boolean;
  onSelectRun: (run: RunRecord) => void;
  onCancelRun: (runId: string) => void;
  onRefresh: () => void;
}

const RunsTable: React.FC<RunsTableProps> = ({
  runs,
  loading,
  onSelectRun,
  onCancelRun,
  onRefresh,
}) => (
  <div className="space-y-4">
    <div className="flex items-center justify-between">
      <div>
        <h2 className="text-xl font-bold text-slate-100">Pipeline Executions</h2>
        <p className="text-xs text-slate-400 mt-0.5">
          Recent build and deployment runs across branches.
        </p>
      </div>
      <button
        onClick={onRefresh}
        className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-slate-900 hover:bg-slate-850 border border-slate-800 text-xs text-slate-300 font-medium transition-colors"
      >
        <RefreshCw className="w-3.5 h-3.5" />
        Refresh
      </button>
    </div>

    <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm">
      {loading ? (
        <div className="flex items-center justify-center p-12 text-slate-400">
          <Loader2 className="w-6 h-6 animate-spin mr-2 text-blue-400" />
          Loading pipeline runs...
        </div>
      ) : runs.length === 0 ? (
        <div className="flex flex-col items-center justify-center p-12 text-slate-500">
          <Layers className="w-10 h-10 mb-3 opacity-40" />
          <p className="text-base font-medium">No runs recorded yet</p>
          <p className="text-xs text-slate-600 mt-1">
            Trigger a pipeline manually or push code to your configured GitHub repository.
          </p>
        </div>
      ) : (
        <table className="w-full text-left text-sm text-slate-300">
          <thead className="bg-slate-950/60 text-xs uppercase text-slate-400 font-semibold border-b border-slate-800">
            <tr>
              <th className="px-6 py-3">Status</th>
              <th className="px-6 py-3">Run ID</th>
              <th className="px-6 py-3">Commit & Branch</th>
              <th className="px-6 py-3">Trigger</th>
              <th className="px-6 py-3">Duration</th>
              <th className="px-6 py-3">Started</th>
              <th className="px-6 py-3 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800/60">
            {runs.map((r) => (
              <tr
                key={r.id}
                onClick={() => onSelectRun(r)}
                className="hover:bg-slate-850/50 cursor-pointer transition-colors"
              >
                <td className="px-6 py-4">{getStatusBadge(r.status)}</td>
                <td className="px-6 py-4 font-mono text-xs font-semibold text-slate-200">
                  #{r.id.slice(0, 8)}
                </td>
                <td className="px-6 py-4">
                  <div className="flex items-center gap-2">
                    <GitBranch className="w-3.5 h-3.5 text-blue-400 shrink-0" />
                    <span className="font-medium text-slate-200">{r.branch}</span>
                    <span className="font-mono text-xs text-slate-500">
                      ({r.commit_sha.slice(0, 7)})
                    </span>
                  </div>
                </td>
                <td className="px-6 py-4">
                  <span className="text-xs px-2 py-0.5 rounded bg-slate-800 text-slate-400 border border-slate-700/60">
                    {r.trigger}
                  </span>
                </td>
                <td className="px-6 py-4 font-mono text-xs text-slate-400">
                  {formatMs(r.duration_ms)}
                </td>
                <td className="px-6 py-4 text-xs text-slate-500">
                  {new Date(r.created_at).toLocaleTimeString([], {
                    hour: '2-digit',
                    minute: '2-digit',
                    second: '2-digit',
                  })}
                </td>
                <td className="px-6 py-4 text-right" onClick={(e) => e.stopPropagation()}>
                  {r.status === 'running' ? (
                    <button
                      onClick={() => onCancelRun(r.id)}
                      className="px-2.5 py-1 text-xs text-rose-400 hover:text-rose-300 bg-rose-950/40 rounded border border-rose-800/50"
                    >
                      Cancel
                    </button>
                  ) : (
                    <button
                      onClick={() => onSelectRun(r)}
                      className="text-xs text-blue-400 hover:text-blue-300 font-medium"
                    >
                      Inspect →
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  </div>
);

function getStatusBadge(status: RunStatus) {
  switch (status) {
    case 'success':
      return (
        <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-emerald-950/60 text-emerald-400 border border-emerald-800/50">
          <CheckCircle2 className="w-3.5 h-3.5" />
          Kazi imefaulu (Success)
        </span>
      );
    case 'failure':
    case 'error':
      return (
        <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-rose-950/60 text-rose-400 border border-rose-800/50">
          <XCircle className="w-3.5 h-3.5" />
          Kazi imeshindwa (Failed)
        </span>
      );
    case 'running':
      return (
        <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-blue-950/60 text-blue-400 border border-blue-800/50">
          <Loader2 className="w-3.5 h-3.5 animate-spin" />
          Inaendelea (Running)
        </span>
      );
    case 'skipped':
    case 'cancelled':
      return (
        <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-slate-800 text-slate-400 border border-slate-700">
          <MinusCircle className="w-3.5 h-3.5" />
          {status}
        </span>
      );
    case 'pending':
    default:
      return (
        <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-amber-950/50 text-amber-300 border border-amber-800/40">
          <Clock className="w-3.5 h-3.5" />
          Pending
        </span>
      );
  }
}

function formatMs(ms: number): string {
  if (!ms || ms <= 0) return '0s';
  if (ms < 1000) return `${ms}ms`;
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  const remSec = sec % 60;
  return `${min}m ${remSec}s`;
}

