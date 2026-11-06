// path: web/src/components/DAGView.tsx

import React from 'react';
import { StepRecord, StepStatus } from '../types';
import { CheckCircle2, XCircle, Loader2, Clock, MinusCircle, Layers } from 'lucide-react';

interface DAGViewProps {
  steps: StepRecord[];
  selectedStepId: string | null;
  onSelectStep: (stepId: string | null) => void;
}

export const DAGView: React.FC<DAGViewProps> = ({
  steps,
  selectedStepId,
  onSelectStep,
}) => {
  if (!steps || steps.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center p-8 border border-dashed border-slate-800 rounded-xl bg-slate-900/40 text-slate-400">
        <Layers className="w-8 h-8 text-slate-500 mb-2" />
        <p className="text-sm">No pipeline steps registered for this run.</p>
      </div>
    );
  }

  // Group steps into parallel tiers:
  // If depends_on is defined, group by dependency depth, otherwise arrange linearly
  const tiers: StepRecord[][] = [];
  const processed = new Set<string>();

  // Assign tier depth
  const depthMap = new Map<string, number>();
  for (const s of steps) {
    let depth = 0;
    if (s.depends_on && s.depends_on.length > 0) {
      for (const dep of s.depends_on) {
        depth = Math.max(depth, (depthMap.get(dep) || 0) + 1);
      }
    }
    depthMap.set(s.step_id, depth);
  }

  const maxDepth = Math.max(0, ...Array.from(depthMap.values()));
  for (let d = 0; d <= maxDepth; d++) {
    const tierSteps = steps.filter((s) => (depthMap.get(s.step_id) || 0) === d);
    if (tierSteps.length > 0) {
      tiers.push(tierSteps);
      tierSteps.forEach((s) => processed.add(s.step_id));
    }
  }

  // Any remaining steps fallback to last tier
  const remaining = steps.filter((s) => !processed.has(s.step_id));
  if (remaining.length > 0) {
    tiers.push(remaining);
  }

  return (
    <div className="relative w-full overflow-x-auto p-4 bg-slate-900/60 rounded-xl border border-slate-800">
      <div className="flex items-center justify-between pb-3 mb-4 border-b border-slate-800/80">
        <div className="flex items-center gap-2">
          <Layers className="w-4 h-4 text-blue-400" />
          <span className="text-xs font-semibold text-slate-300 uppercase tracking-wider">
            Pipeline DAG Graph
          </span>
        </div>
        <div className="flex items-center gap-3 text-xs text-slate-400">
          <button
            onClick={() => onSelectStep(null)}
            className={`px-2 py-0.5 rounded text-xs transition-colors ${
              selectedStepId === null
                ? 'bg-blue-600/30 text-blue-300 border border-blue-500/40'
                : 'hover:text-slate-200'
            }`}
          >
            All Steps
          </button>
          <span>•</span>
          <span>Click node to isolate step logs</span>
        </div>
      </div>

      <div className="flex items-center justify-start gap-12 min-w-max py-2 px-4">
        {tiers.map((tier, tierIdx) => (
          <React.Fragment key={tierIdx}>
            <div className="flex flex-col gap-4">
              {tier.map((step) => {
                const isSelected = selectedStepId === step.step_id;
                return (
                  <button
                    key={step.id || step.step_id}
                    onClick={() => onSelectStep(isSelected ? null : step.step_id)}
                    className={`relative text-left w-64 p-3 rounded-lg border transition-all duration-150 ${getNodeStyling(
                      step.status,
                      isSelected
                    )}`}
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="flex items-center gap-2 truncate">
                        {getStatusIcon(step.status)}
                        <span className="font-semibold text-sm text-slate-200 truncate">
                          {step.step_id}
                        </span>
                      </div>
                      <span className="text-[10px] font-mono uppercase px-1.5 py-0.5 rounded border border-slate-700 bg-slate-800/80 text-slate-300">
                        {formatDuration(step.duration_ms)}
                      </span>
                    </div>

                    <div className="text-xs text-slate-400 truncate mt-1">
                      {step.name || step.command}
                    </div>

                    <div className="mt-2.5 pt-2 border-t border-slate-800/60 flex items-center justify-between text-[11px] text-slate-500 font-mono">
                      <span className="truncate max-w-[140px]">{step.image}</span>
                      {step.exit_code !== undefined && (
                        <span
                          className={
                            step.exit_code === 0 ? 'text-emerald-400' : 'text-rose-400'
                          }
                        >
                          exit {step.exit_code}
                        </span>
                      )}
                    </div>
                  </button>
                );
              })}
            </div>

            {tierIdx < tiers.length - 1 && (
              <div className="flex items-center justify-center text-slate-600">
                <svg width="32" height="24" viewBox="0 0 32 24" fill="none">
                  <path
                    d="M2 12H26M26 12L18 4M26 12L18 20"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </svg>
              </div>
            )}
          </React.Fragment>
        ))}
      </div>
    </div>
  );
};

function getStatusIcon(status: StepStatus) {
  switch (status) {
    case 'success':
      return <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />;
    case 'failure':
      return <XCircle className="w-4 h-4 text-rose-400 shrink-0" />;
    case 'running':
      return <Loader2 className="w-4 h-4 text-blue-400 animate-spin shrink-0" />;
    case 'skipped':
    case 'cancelled':
      return <MinusCircle className="w-4 h-4 text-slate-500 shrink-0" />;
    case 'pending':
    default:
      return <Clock className="w-4 h-4 text-amber-400/80 shrink-0" />;
  }
}

function getNodeStyling(status: StepStatus, isSelected: boolean): string {
  const base = isSelected
    ? 'ring-2 ring-blue-500 shadow-lg shadow-blue-500/10 '
    : 'hover:border-slate-700 ';

  switch (status) {
    case 'success':
      return base + 'bg-emerald-950/20 border-emerald-800/50 hover:bg-emerald-950/30';
    case 'failure':
      return base + 'bg-rose-950/20 border-rose-800/50 hover:bg-rose-950/30';
    case 'running':
      return base + 'bg-blue-950/30 border-blue-600 animate-pulse';
    case 'skipped':
    case 'cancelled':
      return base + 'bg-slate-900/40 border-slate-800 text-slate-500 opacity-70';
    case 'pending':
    default:
      return base + 'bg-slate-900 border-slate-800 hover:bg-slate-800/60';
  }
}

function formatDuration(ms: number): string {
  if (!ms || ms <= 0) return '0s';
  if (ms < 1000) return `${ms}ms`;
  const sec = (ms / 1000).toFixed(1);
  return `${sec}s`;
}

