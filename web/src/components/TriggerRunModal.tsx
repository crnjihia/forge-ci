// path: web/src/components/TriggerRunModal.tsx

import React, { useState } from 'react';
import { Play, X, Loader2, Code2 } from 'lucide-react';
import { triggerRun } from '../api';
import { RunRecord } from '../types';

interface TriggerRunModalProps {
  isOpen: boolean;
  onClose: () => void;
  onRunTriggered: (run: RunRecord) => void;
}

const DEFAULT_PIPELINE_YAML = `version: 1
name: forge-ci-pipeline

steps:
  - id: lint
    name: Code Quality Check
    image: golang:1.22-alpine
    run: go vet ./...

  - id: test
    name: Unit & Integration Tests
    image: golang:1.22-alpine
    run: go test -v ./...
    depends-on: [lint]

  - id: build
    name: Binary Compilation
    image: golang:1.22-alpine
    run: go build -o dist/app ./...
    depends-on: [test]
`;

export const TriggerRunModal: React.FC<TriggerRunModalProps> = ({
  isOpen,
  onClose,
  onRunTriggered,
}) => {
  const [repoUrl, setRepoUrl] = useState('https://github.com/crnji/forge-ci.git');
  const [branch, setBranch] = useState('main');
  const [commitSha, setCommitSha] = useState('');
  const [pipelineYaml, setPipelineYaml] = useState(DEFAULT_PIPELINE_YAML);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!isOpen) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      setSubmitting(true);
      setError(null);
      const newRun = await triggerRun({
        repo_url: repoUrl.trim(),
        branch: branch.trim(),
        commit_sha: commitSha.trim() || undefined,
        pipeline_yaml: pipelineYaml.trim() || undefined,
      });
      onRunTriggered(newRun);
      onClose();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to trigger pipeline run');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/75 backdrop-blur-sm">
      <div className="w-full max-w-2xl bg-slate-900 border border-slate-800 rounded-2xl shadow-2xl overflow-hidden flex flex-col max-h-[90vh]">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-slate-800">
          <div className="flex items-center gap-2">
            <Play className="w-5 h-5 text-blue-400" />
            <h3 className="text-lg font-bold text-slate-100">Trigger Pipeline Execution</h3>
          </div>
          <button onClick={onClose} className="text-slate-500 hover:text-slate-300">
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Form Body */}
        <form onSubmit={handleSubmit} className="flex-1 overflow-y-auto p-6 space-y-4">
          {error && (
            <div className="p-3.5 bg-rose-950/40 border border-rose-800 rounded-xl text-xs text-rose-300">
              {error}
            </div>
          )}

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div>
              <label className="block text-xs font-semibold text-slate-300 uppercase mb-1">
                Repository URL
              </label>
              <input
                type="text"
                value={repoUrl}
                onChange={(e) => setRepoUrl(e.target.value)}
                required
                className="w-full px-3 py-2 bg-slate-950 border border-slate-800 rounded-lg text-sm text-slate-100 font-mono focus:outline-none focus:border-blue-500"
              />
            </div>

            <div>
              <label className="block text-xs font-semibold text-slate-300 uppercase mb-1">
                Branch
              </label>
              <input
                type="text"
                value={branch}
                onChange={(e) => setBranch(e.target.value)}
                required
                className="w-full px-3 py-2 bg-slate-950 border border-slate-800 rounded-lg text-sm text-slate-100 font-mono focus:outline-none focus:border-blue-500"
              />
            </div>
          </div>

          <div>
            <label className="block text-xs font-semibold text-slate-300 uppercase mb-1">
              Commit SHA (Optional - defaults to auto-generated)
            </label>
            <input
              type="text"
              placeholder="e.g. 4f3c8a901234567890abcdef"
              value={commitSha}
              onChange={(e) => setCommitSha(e.target.value)}
              className="w-full px-3 py-2 bg-slate-950 border border-slate-800 rounded-lg text-sm text-slate-100 font-mono focus:outline-none focus:border-blue-500"
            />
          </div>

          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="block text-xs font-semibold text-slate-300 uppercase flex items-center gap-1.5">
                <Code2 className="w-3.5 h-3.5 text-blue-400" />
                Pipeline Definition (YAML)
              </label>
              <span className="text-[11px] text-slate-500 font-mono">Strict DAG Validation</span>
            </div>
            <textarea
              rows={8}
              value={pipelineYaml}
              onChange={(e) => setPipelineYaml(e.target.value)}
              className="w-full p-3 bg-slate-950 border border-slate-800 rounded-lg text-xs text-slate-200 font-mono leading-relaxed focus:outline-none focus:border-blue-500"
            />
          </div>

          <div className="flex items-center justify-end gap-3 pt-4 border-t border-slate-800">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 rounded-lg text-sm font-medium text-slate-400 hover:text-slate-200 transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={submitting}
              className="flex items-center gap-2 px-5 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium transition-colors disabled:opacity-50 shadow-lg shadow-blue-600/20"
            >
              {submitting ? (
                <>
                  <Loader2 className="w-4 h-4 animate-spin" />
                  Triggering Run...
                </>
              ) : (
                <>
                  <Play className="w-4 h-4" />
                  Execute Pipeline
                </>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};

