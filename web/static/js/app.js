// Gridora frontend — unified dashboard with SSE, Chart.js, custom plugins
const Gridora = (() => {
    const COLORS = {
        consumption: '#f97316',
        generation:  '#22c55e',
        export:      '#facc15',
        import:      '#ef4444',
        diverted:    '#3b82f6',
    };
    const ALPHA = '14'; // ~8% opacity hex suffix for fill

    // Dark theme defaults for Chart.js
    Chart.defaults.color = '#64748b';
    Chart.defaults.borderColor = '#334155';

    // Clamp negative values to zero for calculations (CT clamp noise).
    // Raw values are still displayed — this only affects derived numbers.
    const pos = (v) => Math.max(0, v || 0);

    // Display kWh values, falling back to Wh when < 1 kWh for readability.
    function setEnergy(valueId, kwh) {
        const el = document.getElementById(valueId);
        if (!el) return;
        const unitEl = el.parentElement.querySelector('.unit');
        if (Math.abs(kwh) < 1 && kwh !== 0) {
            el.textContent = Math.round(kwh * 1000);
            if (unitEl) unitEl.textContent = 'Wh';
        } else {
            el.textContent = kwh.toFixed(1);
            if (unitEl) unitEl.textContent = 'kWh';
        }
    }

    let chart = null;
    let serial = '';
    let milestones = [];
    let liveMode = true;
    let refreshTimer = null;
    let filterAfter = null;

    function init(s, ms) {
        serial = s;
        milestones = ms || [];
        chart = createChart();
        buildLegend();
        startSSE();
        bindControls();

        if (!restoreFromURL()) {
            selectRange('today');
        }

        window.addEventListener('popstate', () => restoreFromURL());
    }

    // ── Loading bar ──────────────────────────────

    function showLoading() {
        const el = document.getElementById('loading-bar');
        if (el) el.classList.add('active');
    }

    function hideLoading() {
        const el = document.getElementById('loading-bar');
        if (el) el.classList.remove('active');
    }

    // ── SSE (always connected, gauges shown/hidden) ──

    function startSSE() {
        const url = '/api/v1/status/stream' + (serial ? '?serial=' + serial : '');
        const source = new EventSource(url);

        source.onmessage = (e) => updateGauges(JSON.parse(e.data));
        source.onerror = () => {
            document.getElementById('status-bar').textContent = 'Connection lost. Reconnecting...';
        };
        source.onopen = () => {
            document.getElementById('status-bar').textContent = 'Connected';
        };
    }

    // Toggle a live tile value and indicator on the parent card
    function setLiveTile(id, text, active) {
        const el = document.getElementById(id);
        if (!el) return;
        el.textContent = text;
        const card = el.closest('.gauge-card');
        if (card) card.classList.toggle('live-active', active);
    }

    // ── Mini electricity meter (mechanical counter) ──
    // Red digit scrolls continuously; white digits click on carry-over.
    let meterRaf = null;
    let meterStart = null;
    let meterPrevDigits = [0, 0, 0, 0];
    let meterRolls = null;    // cached roll elements
    const DIGIT_H = 11;       // matches .meter-digit height / line-height
    const CYCLE_MS = 16000;   // time for the red digit to complete one 0→9 rotation

    function startMeter() {
        if (meterRaf) return;
        const meter = document.getElementById('import-meter');
        if (!meter) return;

        meterStart = null;
        meterPrevDigits = [0, 0, 0, 0];
        meterRolls = meter.querySelectorAll('.meter-roll');
        meterRolls.forEach(r => {
            r.style.transition = 'none';
            r.style.transform = 'translateY(0)';
        });
        meter.classList.add('active');
        meterRaf = requestAnimationFrame(meterFrame);
    }

    function stopMeter() {
        if (meterRaf) { cancelAnimationFrame(meterRaf); meterRaf = null; }
        meterStart = null;
        const meter = document.getElementById('import-meter');
        if (meter) meter.classList.remove('active');
    }

    function meterFrame(now) {
        if (!meterStart) meterStart = now;
        const elapsed = now - meterStart;

        // Red digit: continuous fractional position (0.0 → 9.999…)
        const redPos = (elapsed % CYCLE_MS) / CYCLE_MS * 10;

        // Number of completed red cycles → drives white digit carry-over
        const cycles = Math.floor(elapsed / CYCLE_MS);

        if (meterRolls && meterRolls.length === 5) {
            const rolls = meterRolls;
            // Red digit — smooth, no CSS transition, updated every frame
            rolls[4].style.transition = 'none';
            rolls[4].style.transform = 'translateY(-' + (redPos * DIGIT_H) + 'px)';

            // White digits — step with transition on carry-over
            const digits = [
                Math.floor(cycles / 1000) % 10,
                Math.floor(cycles / 100) % 10,
                Math.floor(cycles / 10) % 10,
                cycles % 10,
            ];

            digits.forEach((d, i) => {
                if (d === meterPrevDigits[i]) return;
                const roll = rolls[i];
                roll.style.transition = 'transform 0.4s ease';

                if (meterPrevDigits[i] === 9 && d === 0) {
                    // Wrap: scroll to duplicate "0" at position 10, then snap
                    roll.style.transform = 'translateY(-' + (10 * DIGIT_H) + 'px)';
                    setTimeout(() => {
                        roll.style.transition = 'none';
                        roll.style.transform = 'translateY(0)';
                    }, 400);
                } else {
                    roll.style.transform = 'translateY(-' + (d * DIGIT_H) + 'px)';
                }
                meterPrevDigits[i] = d;
            });
        }

        meterRaf = requestAnimationFrame(meterFrame);
    }

    function updateGauges(d) {
        const gridW = Math.round(d.grid_w);
        const genW = Math.round(d.generation_w);
        const exportW = gridW < 0 ? Math.abs(gridW) : 0;
        const importW = gridW > 0 ? gridW : 0;
        const consumptionW = Math.max(0, Math.round(pos(d.generation_w) + d.grid_w));

        // 4 live power tiles — continuous pulse when value > 0
        setLiveTile('consumption-value', consumptionW.toLocaleString(), consumptionW > 0);
        setLiveTile('gen-value', genW.toLocaleString(), genW > 0);
        setLiveTile('grid-export-value', exportW.toLocaleString(), exportW > 0);
        setLiveTile('grid-import-value', importW.toLocaleString(), importW > 0);

        // Mini electricity meter — runs only when importing
        if (importW > 0) { startMeter(); } else { stopMeter(); }

        // Remaining tiles — plain text, no animation
        const divEl = document.getElementById('div-value');
        if (divEl) divEl.textContent = Math.round(d.diversion_w).toLocaleString();
        const voltEl = document.getElementById('voltage-value');
        if (voltEl) voltEl.textContent = d.voltage.toFixed(1);
        const modeEl = document.getElementById('mode-value');
        if (modeEl) modeEl.textContent = d.zappi_mode_name;
        const chargeEl = document.getElementById('charge-value');
        if (chargeEl) chargeEl.textContent = d.charge_added_kwh.toFixed(1);

        document.getElementById('status-bar').textContent = 'Last update: ' + d.timestamp;
    }

    function setLiveMode(on) {
        liveMode = on;
        document.getElementById('live-gauges').style.display = on ? '' : 'none';
        document.getElementById('status-bar').style.display = on ? '' : 'none';

        if (refreshTimer) clearInterval(refreshTimer);
        if (on) {
            refreshTimer = setInterval(loadChart, 5 * 60 * 1000);
        }
    }

    // ── Chart.js plugins ─────────────────────────

    const crosshairPlugin = {
        id: 'crosshair',
        afterDraw(chart) {
            if (!chart._active || !chart._active.length) return;
            const ctx = chart.ctx;
            const active = chart._active[0];
            const x = active.element.x;
            const area = chart.chartArea;

            ctx.save();
            ctx.setLineDash([4, 4]);
            ctx.lineWidth = 1;
            ctx.strokeStyle = '#64748b';
            ctx.beginPath();
            ctx.moveTo(x, area.top);
            ctx.lineTo(x, area.bottom);
            ctx.stroke();
            ctx.restore();
        }
    };

    const milestonePlugin = {
        id: 'milestones',
        afterDraw(chart) {
            if (!milestones || !milestones.length) return;
            const xScale = chart.scales.x;
            if (!xScale) return;
            const area = chart.chartArea;
            const ctx = chart.ctx;

            milestones.forEach(ms => {
                const ts = new Date(ms.date + 'T00:00:00').getTime();
                const x = xScale.getPixelForValue(ts);

                // Only draw if within visible area
                if (x < area.left || x > area.right) return;

                ctx.save();
                ctx.setLineDash([6, 3]);
                ctx.lineWidth = 2;
                ctx.strokeStyle = '#e2e8f0';
                ctx.beginPath();
                ctx.moveTo(x, area.top);
                ctx.lineTo(x, area.bottom);
                ctx.stroke();

                ctx.fillStyle = '#e2e8f0';
                ctx.font = '11px sans-serif';
                ctx.fillText(ms.label, x + 4, area.top + 14);
                ctx.restore();
            });
        }
    };

    // ── External tooltip ─────────────────────────

    function externalTooltip(context) {
        let el = document.getElementById('chart-tooltip');
        if (!el) {
            el = document.createElement('div');
            el.id = 'chart-tooltip';
            document.body.appendChild(el);
        }

        const tooltip = context.tooltip;
        if (tooltip.opacity === 0) {
            el.style.opacity = '0';
            return;
        }

        // Build tooltip using safe DOM methods
        el.textContent = ''; // clear previous content

        const titleDiv = document.createElement('div');
        titleDiv.style.cssText = 'color:#f8fafc;font-weight:600;margin-bottom:4px';
        titleDiv.textContent = tooltip.title[0];
        el.appendChild(titleDiv);

        tooltip.dataPoints.forEach(dp => {
            const row = document.createElement('div');
            row.style.cssText = 'display:flex;align-items:center;margin-top:2px';

            const color = dp.dataset.borderColor;
            const box = document.createElement('span');
            box.style.cssText = 'display:inline-flex;align-items:center;justify-content:center;width:14px;height:14px;border:2px solid ' + color + ';border-radius:3px;margin-right:6px;flex-shrink:0';

            const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
            svg.setAttribute('width', '8');
            svg.setAttribute('height', '8');
            svg.setAttribute('viewBox', '0 0 10 10');
            const poly = document.createElementNS('http://www.w3.org/2000/svg', 'polyline');
            poly.setAttribute('points', '1,5 4,8 9,2');
            poly.setAttribute('fill', 'none');
            poly.setAttribute('stroke', color);
            poly.setAttribute('stroke-width', '2');
            svg.appendChild(poly);
            box.appendChild(svg);

            const label = document.createElement('span');
            label.textContent = dp.dataset.label + ': ';

            const val = document.createElement('strong');
            val.style.marginLeft = '4px';
            val.textContent = dp.formattedValue + ' ' + chart.options.scales.y.title.text;

            row.appendChild(box);
            row.appendChild(label);
            row.appendChild(val);
            el.appendChild(row);
        });

        el.style.opacity = '1';
        const pos = context.chart.canvas.getBoundingClientRect();
        el.style.left = pos.left + window.scrollX + tooltip.caretX + 12 + 'px';
        el.style.top = pos.top + window.scrollY + tooltip.caretY - 20 + 'px';
    }

    // ── Chart creation ───────────────────────────

    function createChart() {
        const ctx = document.getElementById('mainChart');
        if (!ctx) return null;

        return new Chart(ctx, {
            type: 'line',
            plugins: [crosshairPlugin, milestonePlugin],
            data: {
                datasets: [
                    { label: 'Consumption', data: [], borderColor: COLORS.consumption, backgroundColor: COLORS.consumption + ALPHA, fill: true, tension: 0.3, pointRadius: 0, pointHitRadius: 8, borderWidth: 2 },
                    { label: 'Generation',  data: [], borderColor: COLORS.generation,  backgroundColor: COLORS.generation + ALPHA,  fill: true, tension: 0.3, pointRadius: 0, pointHitRadius: 8, borderWidth: 2 },
                    { label: 'Grid Export',  data: [], borderColor: COLORS.export,      backgroundColor: COLORS.export + ALPHA,      fill: true, tension: 0.3, pointRadius: 0, pointHitRadius: 8, borderWidth: 2 },
                    { label: 'Grid Import',  data: [], borderColor: COLORS.import,      backgroundColor: COLORS.import + ALPHA,      fill: true, tension: 0.3, pointRadius: 0, pointHitRadius: 8, borderWidth: 2 },
                    { label: 'EV Charging',  data: [], borderColor: COLORS.diverted,    backgroundColor: COLORS.diverted + ALPHA,    fill: true, tension: 0.3, pointRadius: 0, pointHitRadius: 8, borderWidth: 2 },
                ],
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: { intersect: false, mode: 'index' },
                scales: {
                    x: {
                        type: 'time',
                        time: { unit: 'hour', displayFormats: { hour: 'HH:mm', day: 'dd/MM', week: 'dd/MM', month: 'MMM yyyy', quarter: 'QQQ yyyy', year: 'yyyy' } },
                        grid: { color: '#1e293b' },
                        ticks: { color: '#64748b', maxRotation: 45, font: { size: 10 } },
                    },
                    y: {
                        title: { display: true, text: 'kWh', color: '#64748b' },
                        beginAtZero: true,
                        grid: { color: '#334155' },
                        ticks: { color: '#64748b' },
                    },
                },
                plugins: {
                    legend: { display: false },
                    tooltip: {
                        enabled: false,
                        external: externalTooltip,
                    },
                },
            },
        });
    }

    // ── Custom checkbox legend ────────────────────

    function buildLegend() {
        if (!chart) return;
        const legendEl = document.getElementById('chart-legend');
        if (!legendEl) return;
        legendEl.textContent = '';

        chart.data.datasets.forEach((ds, i) => {
            const item = document.createElement('div');
            item.className = 'legend-item';

            const box = document.createElement('span');
            box.className = 'legend-checkbox';
            box.style.borderColor = ds.borderColor;

            const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
            svg.setAttribute('width', '10');
            svg.setAttribute('height', '10');
            svg.setAttribute('viewBox', '0 0 10 10');
            const poly = document.createElementNS('http://www.w3.org/2000/svg', 'polyline');
            poly.setAttribute('points', '1,5 4,8 9,2');
            poly.setAttribute('fill', 'none');
            poly.setAttribute('stroke', ds.borderColor);
            poly.setAttribute('stroke-width', '2');
            svg.appendChild(poly);
            box.appendChild(svg);

            const label = document.createElement('span');
            label.textContent = ds.label;

            item.appendChild(box);
            item.appendChild(label);

            item.addEventListener('click', () => {
                const visible = chart.isDatasetVisible(i);
                chart.setDatasetVisibility(i, !visible);
                item.classList.toggle('hidden', visible);
                chart.update();
            });

            legendEl.appendChild(item);
        });
    }

    // ── Controls ─────────────────────────────────

    function bindControls() {
        document.querySelectorAll('.quick-range button').forEach(btn => {
            btn.addEventListener('click', (e) => {
                document.querySelectorAll('.quick-range button').forEach(b => b.classList.remove('active'));
                e.target.classList.add('active');
                selectRange(e.target.dataset.range);
            });
        });

        document.getElementById('from-date').addEventListener('change', () => {
            clearActiveButton();
            filterAfter = null;
            setLiveMode(false);
            autoResolution();
            loadChart();
        });
        document.getElementById('to-date').addEventListener('change', () => {
            clearActiveButton();
            filterAfter = null;
            setLiveMode(false);
            autoResolution();
            loadChart();
        });
        document.getElementById('resolution').addEventListener('change', loadChart);
        document.getElementById('include-ev').addEventListener('change', loadChart);
    }

    function clearActiveButton() {
        document.querySelectorAll('.quick-range button').forEach(b => b.classList.remove('active'));
    }

    // Auto-select resolution based on date span
    function autoResolution() {
        const from = document.getElementById('from-date').value;
        const to = document.getElementById('to-date').value;
        if (!from || !to) return;

        const days = (new Date(to) - new Date(from)) / (1000 * 60 * 60 * 24);
        let resolution;
        if (days <= 2) resolution = 'minute';
        else if (days <= 14) resolution = 'hourly';
        else if (days <= 90) resolution = 'daily';
        else if (days <= 365) resolution = 'weekly';
        else if (days <= 730) resolution = 'monthly';
        else if (days <= 1825) resolution = 'quarterly';
        else resolution = 'yearly';

        document.getElementById('resolution').value = resolution;
    }

    function selectRange(range) {
        const now = new Date();
        const today = isoDate(now);
        let from, to, resolution;

        switch (range) {
            case '12h': {
                const twelveAgo = new Date(now.getTime() - 12 * 60 * 60 * 1000);
                from = isoDate(twelveAgo);
                to = today;
                resolution = 'minute';
                filterAfter = twelveAgo.toISOString();
                setLiveMode(true);
                break;
            }
            case 'today':
                from = today;
                to = today;
                resolution = 'minute';
                filterAfter = null;
                setLiveMode(true);
                break;
            case 'yesterday': {
                const y = new Date(now);
                y.setDate(y.getDate() - 1);
                from = isoDate(y);
                to = isoDate(y);
                resolution = 'hourly';
                filterAfter = null;
                setLiveMode(false);
                break;
            }
            case 'thisyear': {
                from = now.getFullYear() + '-01-01';
                to = today;
                resolution = 'daily';
                filterAfter = null;
                setLiveMode(false);
                break;
            }
            case 'all':
                from = '2000-01-01';
                to = today;
                resolution = 'monthly';
                filterAfter = null;
                setLiveMode(false);
                break;
            default: {
                const days = parseInt(range);
                const d = new Date(now);
                d.setDate(d.getDate() - days);
                from = isoDate(d);
                to = today;
                if (days <= 2) resolution = 'minute';
                else if (days <= 14) resolution = 'hourly';
                else resolution = 'daily';
                filterAfter = null;
                setLiveMode(false);
                break;
            }
        }

        document.getElementById('from-date').value = from;
        document.getElementById('to-date').value = to;
        document.getElementById('resolution').value = resolution;
        loadChart();
    }

    // ── Period string → Date converter ───────────

    function periodToDate(p) {
        if (!p) return null;

        // Weekly: "2025-W14"
        const wm = p.match(/^(\d{4})-W(\d{2})$/);
        if (wm) {
            const year = parseInt(wm[1]);
            const week = parseInt(wm[2]);
            const jan1 = new Date(year, 0, 1);
            const jan1Day = jan1.getDay(); // 0=Sun
            // %W: Monday-based, week 00 has days before first Monday
            const firstMonday = jan1Day <= 1 ? 1 - jan1Day : 8 - jan1Day;
            const dayOfYear = firstMonday + (week - 1) * 7;
            return new Date(year, 0, 1 + dayOfYear);
        }

        // Quarterly: "2025-Q2"
        const qm = p.match(/^(\d{4})-Q(\d)$/);
        if (qm) return new Date(parseInt(qm[1]), (parseInt(qm[2]) - 1) * 3, 1);

        // Yearly: "2025"
        if (/^\d{4}$/.test(p)) return new Date(parseInt(p), 0, 1);

        // Monthly: "2025-04"
        if (/^\d{4}-\d{2}$/.test(p)) return new Date(p + '-01T00:00:00');

        // Daily or datetime fallback
        return new Date(p);
    }

    // ── Chart data loading ───────────────────────

    function loadChart() {
        if (!chart) return;
        const from = document.getElementById('from-date').value;
        const to = document.getElementById('to-date').value;
        const resolution = document.getElementById('resolution').value;
        if (!from || !to) return;

        // Time axis unit mapping
        const units = {
            minute: 'hour', hourly: 'hour', daily: 'day',
            weekly: 'week', monthly: 'month', quarterly: 'quarter', yearly: 'year'
        };
        chart.options.scales.x.time.unit = units[resolution] || 'day';

        const url = `/api/v1/energy/${resolution}?serial=${serial}&from=${from}&to=${to}`;

        showLoading();
        fetch(url)
            .then(r => r.json())
            .then(data => {
                if (!data) data = [];

                // Determine the time key based on resolution
                const isPeriod = ['weekly', 'monthly', 'quarterly', 'yearly'].includes(resolution);
                const timeKey = isPeriod ? 'period' : (resolution === 'daily' ? 'date' : 't');
                const includeEV = document.getElementById('include-ev').checked;

                // Client-side time filter (for "Last 12h")
                if (filterAfter && !isPeriod) {
                    data = data.filter(d => d[timeKey] >= filterAfter);
                }

                // Convert time values to x coordinates
                const toX = (d) => {
                    if (isPeriod) return periodToDate(d[timeKey]);
                    return d[timeKey];
                };

                // All calculations use pos() to clamp negatives from CT noise
                const evTotal = (d) => pos(d.diverted) + pos(d.boosted);
                const evOffset = (d) => includeEV ? 0 : evTotal(d);

                chart.data.datasets[0].data = data.map(d => ({ x: toX(d), y: pos(d.import) + pos(d.generation) - pos(d.export) - evOffset(d) }));
                chart.data.datasets[1].data = data.map(d => ({ x: toX(d), y: pos(d.generation) }));
                chart.data.datasets[2].data = data.map(d => ({ x: toX(d), y: pos(d.export) }));
                chart.data.datasets[3].data = data.map(d => ({ x: toX(d), y: pos(d.import) }));
                chart.data.datasets[4].data = data.map(d => ({ x: toX(d), y: evTotal(d) }));
                chart.data.datasets[4].hidden = !includeEV;

                // Auto-switch to Wh when all values are below 1 kWh
                const maxY = Math.max(...chart.data.datasets.flatMap(ds => ds.data.map(p => p.y)));
                if (maxY < 1 && maxY > 0) {
                    chart.data.datasets.forEach(ds => { ds.data.forEach(p => { p.y *= 1000; }); });
                    chart.options.scales.y.title.text = 'Wh';
                } else {
                    chart.options.scales.y.title.text = 'kWh';
                }

                chart.update();

                updateSummary(data, !includeEV);
                updateExportLinks();
                pushURL();
            })
            .finally(() => hideLoading());
    }

    // ── Summary ──────────────────────────────────

    function updateSummary(data, excludeEV) {
        const el = document.getElementById('summary');
        if (!data || data.length === 0) {
            el.style.display = 'none';
            return;
        }

        // Clamp each data point before accumulating (CT noise protection)
        const totals = data.reduce((acc, d) => {
            acc.import += pos(d.import);
            acc.export += pos(d.export);
            acc.generation += pos(d.generation);
            acc.diverted += pos(d.diverted);
            acc.boosted += pos(d.boosted);
            return acc;
        }, { import: 0, export: 0, generation: 0, diverted: 0, boosted: 0 });

        const evTotal = totals.diverted + totals.boosted;
        let consumption = totals.import + totals.generation - totals.export;
        if (excludeEV) consumption -= evTotal;

        const selfUse = totals.generation > 0
            ? ((totals.generation - totals.export) / totals.generation * 100).toFixed(1)
            : '0.0';

        const consumptionLabel = document.querySelector('#summary .consumption .label');
        if (consumptionLabel) {
            if (excludeEV) {
                consumptionLabel.textContent = 'Consumption';
            } else {
                consumptionLabel.textContent = '';
                consumptionLabel.appendChild(document.createTextNode('Consumption '));
                const evSpan = document.createElement('span');
                evSpan.textContent = '+EV';
                evSpan.style.color = '#3b82f6';
                consumptionLabel.appendChild(evSpan);
            }
        }

        setEnergy('sum-consumption', consumption);
        setEnergy('sum-generation', totals.generation);
        setEnergy('sum-export', totals.export);
        setEnergy('sum-import', totals.import);
        setEnergy('sum-diverted', evTotal);

        const evDetail = document.getElementById('ev-detail');
        if (evDetail) {
            if (evTotal > 0) {
                evDetail.textContent = totals.diverted.toFixed(1) + ' solar \u00b7 ' + totals.boosted.toFixed(1) + ' grid';
                evDetail.style.display = '';
            } else {
                evDetail.style.display = 'none';
            }
        }
        document.getElementById('sum-selfuse').textContent = selfUse;
        el.style.display = '';
    }

    function updateExportLinks() {
        const from = document.getElementById('from-date').value;
        const to = document.getElementById('to-date').value;
        const resolution = document.getElementById('resolution').value;
        const base = '/api/v1/export?serial=' + serial + '&from=' + from + '&to=' + to + '&resolution=' + resolution;

        const csvLink = document.getElementById('export-csv');
        const jsonLink = document.getElementById('export-json');
        if (csvLink) csvLink.href = base + '&format=csv';
        if (jsonLink) jsonLink.href = base + '&format=json';
    }

    // ── URL State ────────────────────────────────

    function pushURL() {
        const from = document.getElementById('from-date').value;
        const to = document.getElementById('to-date').value;
        const resolution = document.getElementById('resolution').value;
        const includeEV = document.getElementById('include-ev').checked;

        const params = new URLSearchParams();
        if (from) params.set('from', from);
        if (to) params.set('to', to);
        if (resolution) params.set('res', resolution);
        if (includeEV) params.set('include-ev', '1');

        const activeBtn = document.querySelector('.quick-range button.active');
        if (activeBtn) params.set('range', activeBtn.dataset.range);

        const url = '/' + (params.toString() ? '?' + params.toString() : '');
        history.replaceState(null, '', url);
    }

    function restoreFromURL() {
        const params = new URLSearchParams(window.location.search);
        if (!params.has('from') && !params.has('range')) return false;

        const includeEV = params.get('include-ev') === '1';
        document.getElementById('include-ev').checked = includeEV;

        const range = params.get('range');
        if (range) {
            clearActiveButton();
            const btn = document.querySelector('.quick-range button[data-range="' + range + '"]');
            if (btn) btn.classList.add('active');
            selectRange(range);
            return true;
        }

        const from = params.get('from');
        const to = params.get('to');
        const resolution = params.get('res') || 'daily';

        if (from) document.getElementById('from-date').value = from;
        if (to) document.getElementById('to-date').value = to;
        document.getElementById('resolution').value = resolution;

        clearActiveButton();
        filterAfter = null;
        setLiveMode(false);
        loadChart();
        return true;
    }

    function isoDate(d) {
        return d.toISOString().slice(0, 10);
    }

    return { init };
})();
