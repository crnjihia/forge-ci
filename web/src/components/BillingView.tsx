// path: web/src/components/BillingView.tsx

import React, { useEffect, useState } from 'react';
import { Smartphone, CheckCircle, AlertCircle, Loader2, RefreshCw } from 'lucide-react';
import { BillingBalance, TopUpResponse } from '../types';
import { fetchBillingBalance, initiateMpesaTopup } from '../api';

export const BillingView: React.FC = () => {
  const [balance, setBalance] = useState<BillingBalance | null>(null);
  const [loading, setLoading] = useState(true);
  const [phone, setPhone] = useState('0712345678');
  const [amount, setAmount] = useState<number>(500);
  const [customAmount, setCustomAmount] = useState<string>('');
  const [submitting, setSubmitting] = useState(false);
  const [topupResult, setTopupResult] = useState<TopUpResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  const loadBalance = async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await fetchBillingBalance('default_user');
      setBalance(data);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to fetch billing balance');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadBalance();
  }, []);

  const handleTopup = async (e: React.FormEvent) => {
    e.preventDefault();
    const finalAmount = customAmount ? parseFloat(customAmount) : amount;
    if (isNaN(finalAmount) || finalAmount <= 0) {
      setError('Please provide a valid top-up amount');
      return;
    }

    try {
      setSubmitting(true);
      setError(null);
      setTopupResult(null);

      const res = await initiateMpesaTopup(phone, finalAmount, 'default_user');
      setTopupResult(res);

      // Refresh balance after brief delay simulating callback
      setTimeout(() => {
        loadBalance();
      }, 4000);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'M-Pesa STK push failed');
    } finally {
      setSubmitting(false);
    }
  };

  const presetAmounts = [100, 250, 500, 1000, 2500];

  return (
    <div className="space-y-6 max-w-4xl">
      {/* Header */}
      <div>
        <h2 className="text-xl font-bold text-slate-100 flex items-center gap-2">
          <Smartphone className="w-5 h-5 text-emerald-400" />
          Kenyan Developer Billing (Safaricom M-Pesa)
        </h2>
        <p className="text-sm text-slate-400 mt-1">
          Pay-as-you-go containerized CI/CD metered at KES 10.00 / compute minute.
        </p>
      </div>

      {/* Credit Balance Card */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div className="p-6 bg-slate-900 border border-slate-800 rounded-2xl shadow-sm relative overflow-hidden">
          <div className="text-xs uppercase font-semibold text-slate-400 mb-1">
            Current Credit Balance
          </div>
          <div className="text-3xl font-extrabold text-emerald-400 font-mono flex items-baseline gap-1">
            {loading ? (
              <Loader2 className="w-6 h-6 animate-spin text-slate-500" />
            ) : (
              <>
                <span className="text-lg text-emerald-500 font-sans">KES</span>
                {balance ? balance.balance_kes.toFixed(2) : '0.00'}
              </>
            )}
          </div>
          <div className="text-xs text-slate-500 mt-2 flex items-center gap-2">
            <span>Currency: KES (Kenyan Shillings)</span>
            <button
              onClick={loadBalance}
              className="text-slate-400 hover:text-slate-200"
              title="Refresh balance"
            >
              <RefreshCw className="w-3 h-3" />
            </button>
          </div>
        </div>

        <div className="p-6 bg-slate-900 border border-slate-800 rounded-2xl shadow-sm">
          <div className="text-xs uppercase font-semibold text-slate-400 mb-1">
            Metered Compute Rate
          </div>
          <div className="text-2xl font-bold text-slate-200 font-mono">
            KES 10.00
            <span className="text-sm text-slate-400 font-sans"> / minute</span>
          </div>
          <div className="text-xs text-slate-500 mt-2">
            Billed strictly per step duration with 1-minute minimum.
          </div>
        </div>

        <div className="p-6 bg-slate-900 border border-slate-800 rounded-2xl shadow-sm">
          <div className="text-xs uppercase font-semibold text-slate-400 mb-1">
            Safaricom Daraja Gateway
          </div>
          <div className="flex items-center gap-2 mt-1">
            <span className="w-2.5 h-2.5 rounded-full bg-emerald-400 animate-pulse" />
            <span className="text-sm font-semibold text-slate-200">Online & Ready</span>
          </div>
          <div className="text-xs text-slate-500 mt-2">
            Instant STK Push to Safaricom & Airtel Kenya mobile lines.
          </div>
        </div>
      </div>

      {error && (
        <div className="p-4 bg-rose-950/40 border border-rose-800 rounded-xl text-sm text-rose-300 flex items-center gap-2">
          <AlertCircle className="w-4 h-4 shrink-0 text-rose-400" />
          <span>{error}</span>
        </div>
      )}

      {topupResult && (
        <div className="p-4 bg-emerald-950/40 border border-emerald-800 rounded-xl text-sm text-emerald-300 space-y-1">
          <div className="flex items-center gap-2 font-semibold text-emerald-200">
            <CheckCircle className="w-4 h-4 text-emerald-400" />
            STK Push Prompt Dispatched! (Tafadhali weka PIN ya M-Pesa)
          </div>
          <p className="text-xs text-emerald-400">
            {topupResult.customer_message ||
              'A prompt has been sent to your phone. Enter your Safaricom M-Pesa PIN to finalize the deposit.'}
          </p>
          <p className="text-[11px] font-mono text-emerald-500">
            Checkout ID: {topupResult.checkout_request_id}
          </p>
        </div>
      )}

      {/* Topup Form */}
      <div className="p-6 bg-slate-900 border border-slate-800 rounded-2xl shadow-sm space-y-6">
        <h3 className="text-base font-bold text-slate-100 flex items-center gap-2">
          <Smartphone className="w-4 h-4 text-emerald-400" />
          Deposit Credits via M-Pesa STK Push
        </h3>

        <form onSubmit={handleTopup} className="space-y-4">
          <div>
            <label className="block text-xs font-semibold text-slate-300 uppercase mb-1">
              Kenyan Mobile Number
            </label>
            <input
              type="text"
              placeholder="+2547XXXXXXXX or 07XXXXXXXX"
              value={phone}
              onChange={(e) => setPhone(e.target.value)}
              required
              className="w-full max-w-md px-3.5 py-2.5 bg-slate-950 border border-slate-800 rounded-xl text-sm text-slate-100 font-mono focus:outline-none focus:border-emerald-500"
            />
            <p className="text-[11px] text-slate-500 mt-1">
              Supports Safaricom (+2547XX, +2541XX) and Airtel lines.
            </p>
          </div>

          <div>
            <label className="block text-xs font-semibold text-slate-300 uppercase mb-2">
              Select Amount (KES)
            </label>
            <div className="flex flex-wrap gap-2">
              {presetAmounts.map((amt) => (
                <button
                  key={amt}
                  type="button"
                  onClick={() => {
                    setAmount(amt);
                    setCustomAmount('');
                  }}
                  className={`px-4 py-2 rounded-xl text-sm font-mono font-medium transition-colors ${
                    amount === amt && !customAmount
                      ? 'bg-emerald-600 text-white shadow-md shadow-emerald-600/30'
                      : 'bg-slate-950 hover:bg-slate-800 text-slate-300 border border-slate-800'
                  }`}
                >
                  KES {amt}
                </button>
              ))}

              <div className="relative">
                <input
                  type="number"
                  placeholder="Custom KES"
                  value={customAmount}
                  onChange={(e) => setCustomAmount(e.target.value)}
                  className="w-32 px-3 py-2 bg-slate-950 border border-slate-800 rounded-xl text-sm text-slate-100 font-mono focus:outline-none focus:border-emerald-500"
                />
              </div>
            </div>
          </div>

          <div className="pt-2">
            <button
              type="submit"
              disabled={submitting}
              className="flex items-center gap-2 px-6 py-2.5 bg-emerald-600 hover:bg-emerald-500 text-white rounded-xl font-medium text-sm transition-colors disabled:opacity-50 shadow-lg shadow-emerald-600/20"
            >
              {submitting ? (
                <>
                  <Loader2 className="w-4 h-4 animate-spin" />
                  Sending STK Push Prompt...
                </>
              ) : (
                <>
                  <Smartphone className="w-4 h-4" />
                  Lipa na M-Pesa (KES {customAmount ? customAmount : amount})
                </>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};

