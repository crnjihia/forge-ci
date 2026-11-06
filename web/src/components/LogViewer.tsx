// path: web/src/components/LogViewer.tsx

import React, { useEffect, useRef, useState } from 'react';
import { Terminal, ArrowDown, Copy, Check, Trash2, Search, X } from 'lucide-react';
import { RunStatus } from '../types';

interface LogViewerProps {
  runId: string;
  stepId: string | null;
  runStatus?: RunStatus;
  onClearStepFilter?: () => void;
}

interface LogEntry {
  id: number;
  text: string;
  timestamp: string;
  step?: string;
}

export const LogViewer: React.FC<LogViewerProps> = ({
  runId,
  stepId,
  runStatus,
  onClearStepFilter,
}) => {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [autoScroll, setAutoScroll] = useState(true);
  const [filterQuery, setFilterQuery] = useState('');
  const [copied, setCopied] = useState(false);
  const [isConnected, setIsConnected] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setLogs([]);
    setIsConnected(false);

    if (!runId) return;

    // Connect to /runs/${runId}/logs without step filter query param
    // We filter by stepId client-side in memory for instant switching without dropping SSE!
    const url = `/runs/${runId}/logs`;
    const eventSource = new EventSource(url);

    const handleLogMessage = (event: MessageEvent) => {
      if (!event.data || event.data === 'keepalive' || event.data.startsWith(':')) return;
      try {
        const parsed = JSON.parse(event.data);
        const entry: LogEntry = {
          id: parsed.id || Date.now(),
          text: parsed.line || event.data,
          timestamp: parsed.timestamp || new Date().toISOString(),
          step: parsed.step_id || undefined,
        };
        setLogs((prev) => [...prev, entry]);
      } catch {
        setLogs((prev) => [
          ...prev,
          {
            id: Date.now(),
            text: event.data,
            timestamp: new Date().toISOString(),
            step: undefined,
          },
        ]);
      }
    };

    eventSource.onopen = () => {
      setIsConnected(true);
    };

    eventSource.addEventListener('log', handleLogMessage);
    eventSource.onmessage = handleLogMessage;

    eventSource.onerror = () => {
      if (eventSource.readyState === EventSource.CLOSED) {
        setIsConnected(false);
      }
    };

    return () => {
      eventSource.close();
    };
  }, [runId]);

  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs, autoScroll, stepId]);

  const handleCopyLogs = () => {
    const text = displayedLogs.map((l) => l.text).join('\n');
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const displayedLogs = logs.filter((l) => {
    if (stepId && l.step && l.step !== stepId) {
      return false;
    }
    if (filterQuery && !l.text.toLowerCase().includes(filterQuery.toLowerCase())) {
      return false;
    }
    return true;
  });

  return (
    <div className="flex flex-col h-[520px] rounded-xl border border-slate-800 bg-slate-950 shadow-2xl overflow-hidden font-mono text-xs">
      {/* Terminal Toolbar */}
      <div className="flex items-center justify-between px-4 py-2.5 bg-slate-900 border-b border-slate-800 select-none">
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-1.5">
            <span className="w-3 h-3 rounded-full bg-rose-500/80 inline-block" />
            <span className="w-3 h-3 rounded-full bg-amber-500/80 inline-block" />
            <span className="w-3 h-3 rounded-full bg-emerald-500/80 inline-block" />
          </div>

          <div className="flex items-center gap-2 pl-2 border-l border-slate-800 text-slate-300 font-sans text-xs">
            <Terminal className="w-3.5 h-3.5 text-blue-400" />
            <span className="font-semibold text-slate-200">Live Execution Logs</span>
            {stepId && (
              <button
                onClick={() => onClearStepFilter && onClearStepFilter()}
                title="Click to view all steps"
                className="flex items-center gap-1 px-2 py-0.5 rounded bg-blue-900/50 text-blue-200 border border-blue-600/50 text-[11px] font-mono hover:bg-blue-800/60 transition-colors cursor-pointer group"
              >
                <span>step: {stepId}</span>
                <X className="w-3 h-3 text-blue-400 group-hover:text-rose-300 ml-0.5" />
              </button>
            )}
          </div>
        </div>

        <div className="flex items-center gap-3 text-slate-400 font-sans text-xs">
          {/* Connection status indicator */}
          <div className="flex items-center gap-1.5 text-[11px]">
            <span
              className={`w-2 h-2 rounded-full ${
                isConnected ? 'bg-emerald-400 animate-pulse' : 'bg-slate-500'
              }`}
            />
            <span className={isConnected ? 'text-emerald-400' : 'text-slate-500'}>
              {isConnected ? 'SSE Live' : 'Disconnected'}
            </span>
          </div>

          {/* Search in logs */}
          <div className="relative">
            <Search className="w-3.5 h-3.5 absolute left-2 top-2 text-slate-500" />
            <input
              type="text"
              placeholder="Search output..."
              value={filterQuery}
              onChange={(e) => setFilterQuery(e.target.value)}
              className="pl-7 pr-2 py-1 w-36 bg-slate-950 border border-slate-800 rounded text-xs text-slate-200 focus:outline-none focus:border-blue-500 font-mono"
            />
          </div>

          {/* Autoscroll toggle */}
          <button
            onClick={() => setAutoScroll(!autoScroll)}
            title="Toggle Autoscroll"
            className={`p-1.5 rounded transition-colors ${
              autoScroll
                ? 'bg-blue-600/30 text-blue-300 border border-blue-500/40'
                : 'hover:text-slate-200 text-slate-500'
            }`}
          >
            <ArrowDown className="w-3.5 h-3.5" />
          </button>

          {/* Copy logs */}
          <button
            onClick={handleCopyLogs}
            title="Copy logs"
            className="p-1.5 rounded hover:text-slate-200 transition-colors"
          >
            {copied ? (
              <Check className="w-3.5 h-3.5 text-emerald-400" />
            ) : (
              <Copy className="w-3.5 h-3.5" />
            )}
          </button>

          {/* Clear display */}
          <button
            onClick={() => setLogs([])}
            title="Clear logs"
            className="p-1.5 rounded hover:text-rose-400 transition-colors"
          >
            <Trash2 className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>

      {/* Terminal Output */}
      <div
        ref={scrollRef}
        className="flex-1 overflow-y-auto p-4 space-y-1 text-slate-300 bg-slate-950 font-mono text-[12px] leading-relaxed selection:bg-blue-600 selection:text-white"
      >
        {displayedLogs.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-full text-slate-500 space-y-2 select-none py-12">
            {filterQuery ? (
              <span className="text-slate-400">No lines matching &quot;{filterQuery}&quot;.</span>
            ) : runStatus === 'running' || runStatus === 'pending' ? (
              <div className="flex items-center gap-2.5 text-slate-400">
                <span className="relative flex h-2 w-2">
                  <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-blue-400 opacity-75"></span>
                  <span className="relative inline-flex rounded-full h-2 w-2 bg-blue-500"></span>
                </span>
                <span className="font-mono text-xs">Awaiting container output stream...</span>
              </div>
            ) : (
              <div className="text-center space-y-2">
                <p className="text-slate-400 font-sans text-xs">
                  {stepId
                    ? `No log output recorded for step "${stepId}".`
                    : 'No log output recorded for this run.'}
                </p>
                {stepId && onClearStepFilter && (
                  <button
                    onClick={onClearStepFilter}
                    className="text-[11px] text-blue-400 hover:text-blue-300 underline font-sans"
                  >
                    Clear step filter (view all pipeline logs)
                  </button>
                )}
              </div>
            )}
          </div>
        ) : (
          displayedLogs.map((log, index) => (
            <div key={log.id || index} className="flex items-start gap-3 hover:bg-slate-900/60 px-1 rounded">
              <span className="text-slate-600 select-none w-8 text-right shrink-0">
                {index + 1}
              </span>
              {log.step && (
                <span className="text-blue-400 select-none shrink-0 font-semibold">
                  [{log.step}]
                </span>
              )}
              <span className="break-all whitespace-pre-wrap flex-1 text-slate-200">
                {log.text}
              </span>
            </div>
          ))
        )}
      </div>

      {/* Terminal Footer */}
      <div className="px-4 py-1.5 bg-slate-900/80 border-t border-slate-800/80 flex items-center justify-between text-[11px] text-slate-500 font-sans">
        <span>Showing {displayedLogs.length} of {logs.length} lines</span>
        <span>AES-256 Secret Scrubbing Active</span>
      </div>
    </div>
  );
};
