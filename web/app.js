// State Management
let state = {
    apiKey: localStorage.getItem('agent_quota_api_key') || '',
    quotas: {},
    history: {},
    activeTab: 'active', // 'active' or 'history'
    selectedQuotaHistory: '',
    autoRefreshInterval: parseInt(localStorage.getItem('agentic_quota_refresh') || '30'),
    activeTimers: [],
    refreshTimerId: null,
    isFetching: false
};

// DOM Elements
const elements = {
    connIndicator: document.getElementById('conn-indicator'),
    connText: document.getElementById('conn-text'),
    settingsBtn: document.getElementById('settings-btn'),
    refreshBtn: document.getElementById('refresh-btn'),
    refreshIcon: document.getElementById('refresh-icon'),
    alertBanner: document.getElementById('alert-banner'),
    alertMsg: document.getElementById('alert-msg'),
    emptyState: document.getElementById('empty-state'),
    quotaGrid: document.getElementById('quota-grid'),
    settingsModal: document.getElementById('settings-modal'),
    modalOverlay: document.getElementById('modal-overlay'),
    closeModalBtn: document.getElementById('close-modal-btn'),
    settingsForm: document.getElementById('settings-form'),
    apiKeyInput: document.getElementById('api-key-input'),
    saveKeyCheckbox: document.getElementById('save-key-checkbox'),
    autoRefreshSelect: document.getElementById('auto-refresh-select'),
    
    // Tabs & Sections
    tabActive: document.getElementById('tab-active'),
    tabHistory: document.getElementById('tab-history'),
    viewActive: document.getElementById('view-active'),
    viewHistory: document.getElementById('view-history'),
    quotaSelect: document.getElementById('quota-select'),
    chartLoading: document.getElementById('chart-loading'),
    chartEmpty: document.getElementById('chart-empty'),
    chartContainer: document.getElementById('chart-container')
};

// SVG Settings for Circle Gauge
const SVG_RADIUS = 50;
const SVG_CIRCUMFERENCE = 2 * Math.PI * SVG_RADIUS; // ~314.16

// SVG Chart Configuration
const CHART_WIDTH = 800;
const CHART_HEIGHT = 380;
const CHART_MARGIN = { top: 40, right: 40, bottom: 50, left: 60 };

// Initialize Application
function init() {
    setupEventListeners();
    
    // Set form and select values from current state
    if (state.apiKey) {
        elements.apiKeyInput.value = state.apiKey;
    }
    elements.autoRefreshSelect.value = state.autoRefreshInterval;
    
    // Initial fetch or prompt for key
    if (!state.apiKey) {
        showModal();
        showBanner('API Key is required to view quota metrics.', 'error');
        elements.quotaGrid.innerHTML = `
            <div class="empty-state" style="margin: 0 auto; width: 100%;">
                <div class="empty-icon">🔑</div>
                <h2>Authentication Required</h2>
                <p>Please click the settings gear or the button below to configure your API key.</p>
                <button onclick="showModal()" class="submit-btn" style="margin-top: 1rem; width: auto; padding: 0.6rem 1.5rem;">Configure API Key</button>
            </div>
        `;
        updateConnectionStatus('offline', 'Authentication Required');
    } else {
        refreshDashboardData();
        setupAutoRefresh();
    }
    
    // Start the countdown ticks (runs independent of fetch)
    startCountdownTicks();
}

// Setup Event Listeners
function setupEventListeners() {
    elements.settingsBtn.addEventListener('click', showModal);
    elements.closeModalBtn.addEventListener('click', hideModal);
    elements.modalOverlay.addEventListener('click', hideModal);
    elements.refreshBtn.addEventListener('click', () => refreshDashboardData(true));
    
    // Tab switching
    elements.tabActive.addEventListener('click', () => switchTab('active'));
    elements.tabHistory.addEventListener('click', () => switchTab('history'));
    
    // Dropdown change for quota selection in history tab
    elements.quotaSelect.addEventListener('change', (e) => {
        state.selectedQuotaHistory = e.target.value;
        drawHistoryChart();
    });
    
    elements.settingsForm.addEventListener('submit', (e) => {
        e.preventDefault();
        const enteredKey = elements.apiKeyInput.value.trim();
        const remember = elements.saveKeyCheckbox.checked;
        
        state.apiKey = enteredKey;
        if (remember) {
            localStorage.setItem('agentic_quota_api_key', enteredKey);
        } else {
            localStorage.removeItem('agentic_quota_api_key');
        }
        
        hideModal();
        refreshDashboardData();
        setupAutoRefresh();
    });
    
    elements.autoRefreshSelect.addEventListener('change', (e) => {
        const value = parseInt(e.target.value);
        state.autoRefreshInterval = value;
        localStorage.setItem('agentic_quota_refresh', value);
        setupAutoRefresh();
    });
}

// Tab switcher
function switchTab(tabId) {
    if (state.activeTab === tabId) return;
    
    state.activeTab = tabId;
    
    if (tabId === 'active') {
        elements.tabActive.classList.add('active');
        elements.tabHistory.classList.remove('active');
        elements.viewActive.classList.remove('hidden');
        elements.viewHistory.classList.add('hidden');
        fetchQuotaData();
    } else {
        elements.tabActive.classList.remove('active');
        elements.tabHistory.classList.add('active');
        elements.viewActive.classList.add('hidden');
        elements.viewHistory.classList.remove('hidden');
        fetchHistoryData();
    }
}

// Modal Controls
function showModal() {
    elements.settingsModal.classList.remove('hidden');
    elements.apiKeyInput.focus();
}

// Close Modal
function hideModal() {
    elements.settingsModal.classList.add('hidden');
}

// Banner controls
function showBanner(message, type = 'error') {
    elements.alertMsg.textContent = message;
    elements.alertBanner.className = 'banner';
    if (type === 'success') {
        elements.alertBanner.classList.add('success');
    }
    elements.alertBanner.classList.remove('hidden');
}

function hideBanner() {
    elements.alertBanner.classList.add('hidden');
}

// Connection Status Helpers
function updateConnectionStatus(status, text) {
    elements.connIndicator.className = 'status-dot';
    elements.connText.textContent = text;
    
    if (status === 'connected') {
        elements.connIndicator.classList.add('status-connected');
    } else if (status === 'unauthorized') {
        elements.connIndicator.classList.add('status-unauthorized');
    } else {
        elements.connIndicator.classList.add('status-offline');
    }
}

// Manage Auto Refresh
function setupAutoRefresh() {
    if (state.refreshTimerId) {
        clearInterval(state.refreshTimerId);
        state.refreshTimerId = null;
    }
    
    if (state.autoRefreshInterval > 0 && state.apiKey) {
        state.refreshTimerId = setInterval(() => {
            refreshDashboardData();
        }, state.autoRefreshInterval * 1000);
    }
}

// Unified Refresh Data Action
function refreshDashboardData(isManual = false) {
    if (state.activeTab === 'active') {
        fetchQuotaData(isManual);
    } else {
        fetchHistoryData(isManual);
    }
}

// Fetch Active Quotas from Server
async function fetchQuotaData(isManual = false) {
    if (state.isFetching) return;
    state.isFetching = true;
    
    elements.refreshIcon.classList.add('spinning');
    if (isManual) {
        elements.refreshBtn.disabled = true;
    }
    
    try {
        const response = await fetch('/api/v1/quota', {
            method: 'GET',
            headers: {
                'X-API-Key': state.apiKey,
                'Accept': 'application/json'
            }
        });
        
        if (response.status === 200) {
            const data = await response.json();
            hideBanner();
            updateConnectionStatus('connected', 'Connected');
            processQuotaData(data.quota || {});
        } else if (response.status === 401) {
            updateConnectionStatus('unauthorized', 'Unauthorized');
            showBanner('Unauthorized API Key. Please verify your X-API-Key settings.');
            showModal();
        } else if (response.status === 404) {
            hideBanner();
            updateConnectionStatus('connected', 'Connected');
            processQuotaData({});
        } else {
            const errData = await response.json().catch(() => ({}));
            const errMsg = errData.error || `Server error (${response.status})`;
            updateConnectionStatus('offline', 'Error');
            showBanner(errMsg);
        }
    } catch (error) {
        console.error('Fetch error:', error);
        updateConnectionStatus('offline', 'Offline');
        showBanner('Cannot connect to quota service. Ensure the server is running.');
    } finally {
        state.isFetching = false;
        elements.refreshIcon.classList.remove('spinning');
        elements.refreshBtn.disabled = false;
    }
}

// Fetch Historical Data from Server
async function fetchHistoryData(isManual = false) {
    if (state.isFetching) return;
    state.isFetching = true;
    
    elements.refreshIcon.classList.add('spinning');
    elements.chartLoading.classList.remove('hidden');
    elements.chartEmpty.classList.add('hidden');
    elements.chartContainer.classList.add('hidden');
    
    if (isManual) {
        elements.refreshBtn.disabled = true;
    }
    
    try {
        const response = await fetch('/api/v1/quota/history', {
            method: 'GET',
            headers: {
                'X-API-Key': state.apiKey,
                'Accept': 'application/json'
            }
        });
        
        if (response.status === 200) {
            const data = await response.json();
            hideBanner();
            updateConnectionStatus('connected', 'Connected');
            state.history = data.history || {};
            populateQuotaDropdown();
            drawHistoryChart();
        } else if (response.status === 401) {
            updateConnectionStatus('unauthorized', 'Unauthorized');
            showBanner('Unauthorized API Key. Please verify your X-API-Key settings.');
            showModal();
        } else {
            const errData = await response.json().catch(() => ({}));
            const errMsg = errData.error || `Server error (${response.status})`;
            updateConnectionStatus('offline', 'Error');
            showBanner(errMsg);
        }
    } catch (error) {
        console.error('History fetch error:', error);
        updateConnectionStatus('offline', 'Offline');
        showBanner('Cannot connect to history service.');
    } finally {
        state.isFetching = false;
        elements.refreshIcon.classList.remove('spinning');
        elements.refreshBtn.disabled = false;
        elements.chartLoading.classList.add('hidden');
    }
}

// Process and parse active metrics
function processQuotaData(quotaMap) {
    state.quotas = quotaMap;
    const keys = Object.keys(quotaMap);
    
    if (keys.length === 0) {
        elements.emptyState.classList.remove('hidden');
        elements.quotaGrid.classList.add('hidden');
        elements.quotaGrid.innerHTML = '';
        state.activeTimers = [];
        return;
    }
    
    elements.emptyState.classList.add('hidden');
    elements.quotaGrid.classList.remove('hidden');
    
    const now = Date.now();
    state.activeTimers = [];
    
    const sortedKeys = keys.sort();
    let gridHTML = '';
    
    sortedKeys.forEach(name => {
        const q = quotaMap[name];
        const targetTime = now + (q.reset_in_seconds * 1000);
        
        state.activeTimers.push({
            name: name,
            targetTime: targetTime
        });
        
        const cardState = getQuotaState(q.remaining_fraction);
        const percentText = (q.remaining_fraction * 100).toFixed(1) + '%';
        const formattedReset = formatTimestamp(q.reset_time);
        const strokeDashoffset = SVG_CIRCUMFERENCE * (1 - q.remaining_fraction);
        
        gridHTML += `
            <div class="quota-card state-${cardState}" data-quota-name="${name}">
                <div class="card-header">
                    <span class="card-title">${name}</span>
                    <span class="card-badge">${cardState}</span>
                </div>
                
                <div class="card-gauge-wrapper">
                    <svg class="gauge-svg" width="130" height="130">
                        <circle class="gauge-track" cx="65" cy="65" r="${SVG_RADIUS}"></circle>
                        <circle class="gauge-value" cx="65" cy="65" r="${SVG_RADIUS}" 
                                stroke-dasharray="${SVG_CIRCUMFERENCE}" 
                                stroke-dashoffset="${strokeDashoffset}"></circle>
                    </svg>
                    <div class="gauge-percentage">
                        <span>${percentText}</span>
                        <span class="gauge-label">remaining</span>
                    </div>
                </div>
                
                <div class="card-details">
                    <div class="detail-row">
                        <span class="detail-label">Reset Time</span>
                        <span class="detail-value" title="${q.reset_time}">${formattedReset}</span>
                    </div>
                    <div class="detail-row">
                        <span class="detail-label">Time Remaining</span>
                        <span class="detail-value countdown" id="countdown-${sanitizeId(name)}">--:--:--</span>
                    </div>
                </div>
            </div>
        `;
    });
    
    elements.quotaGrid.innerHTML = gridHTML;
    tickCountdowns();
}

// Populate history dropdown list
function populateQuotaDropdown() {
    const keys = Object.keys(state.history).sort();
    
    if (keys.length === 0) {
        elements.quotaSelect.innerHTML = '<option value="">No active quotas</option>';
        state.selectedQuotaHistory = '';
        return;
    }
    
    let options = '';
    keys.forEach(key => {
        const isSelected = key === state.selectedQuotaHistory ? 'selected' : '';
        options += `<option value="${key}" ${isSelected}>${key}</option>`;
    });
    elements.quotaSelect.innerHTML = options;
    
    if (!state.selectedQuotaHistory || !keys.includes(state.selectedQuotaHistory)) {
        state.selectedQuotaHistory = keys[0];
    }
}

// Render dynamic SVG graph
function drawHistoryChart() {
    const name = state.selectedQuotaHistory;
    if (!name) {
        elements.chartEmpty.classList.remove('hidden');
        elements.chartContainer.classList.add('hidden');
        return;
    }
    
    const points = state.history[name] || [];
    
    if (points.length === 0) {
        elements.chartEmpty.classList.remove('hidden');
        elements.chartContainer.classList.add('hidden');
        return;
    }
    
    elements.chartEmpty.classList.add('hidden');
    elements.chartContainer.classList.remove('hidden');
    elements.chartContainer.innerHTML = ''; // Clear previous SVG
    
    // Parse timestamps
    const data = points.map(pt => ({
        timestamp: new Date(pt.timestamp).getTime(),
        value: pt.value
    })).sort((a, b) => a.timestamp - b.timestamp);
    
    // Calculate Bounds
    let tMin = data[0].timestamp;
    let tMax = data[data.length - 1].timestamp;
    
    // If only one data point exists, expand horizontal boundary to prevent division by zero
    if (tMin === tMax) {
        tMin -= 3600 * 1000; // -1h
        tMax += 3600 * 1000; // +1h
    }
    
    const chartWidth = CHART_WIDTH - CHART_MARGIN.left - CHART_MARGIN.right;
    const chartHeight = CHART_HEIGHT - CHART_MARGIN.top - CHART_MARGIN.bottom;
    
    // Map timestamps and values into SVG coordinate space
    const projectedPoints = data.map(pt => {
        const x = CHART_MARGIN.left + ((pt.timestamp - tMin) / (tMax - tMin)) * chartWidth;
        const y = CHART_MARGIN.top + (1 - pt.value) * chartHeight; // inverted because 0 is at top
        return { x, y, timestamp: pt.timestamp, value: pt.value };
    });
    
    // Identify state-specific color mapping based on latest value
    const latestVal = data[data.length - 1].value;
    const currentStatus = getQuotaState(latestVal);
    const accentColor = getStatusRGBHex(currentStatus);
    
    // Start generating SVG elements
    const svgNamespace = "http://www.w3.org/2000/svg";
    const svg = document.createElementNS(svgNamespace, "svg");
    svg.setAttribute("viewBox", `0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`);
    svg.setAttribute("class", "chart-svg");
    
    // 1. Defs for area gradients and drop shadow glows
    const defs = document.createElementNS(svgNamespace, "defs");
    
    // Area gradient
    const gradient = document.createElementNS(svgNamespace, "linearGradient");
    gradient.setAttribute("id", "chart-gradient");
    gradient.setAttribute("x1", "0");
    gradient.setAttribute("y1", "0");
    gradient.setAttribute("x2", "0");
    gradient.setAttribute("y2", "1");
    
    const stop0 = document.createElementNS(svgNamespace, "stop");
    stop0.setAttribute("offset", "0%");
    stop0.setAttribute("stop-color", accentColor);
    stop0.setAttribute("stop-opacity", "0.22");
    
    const stop100 = document.createElementNS(svgNamespace, "stop");
    stop100.setAttribute("offset", "100%");
    stop100.setAttribute("stop-color", accentColor);
    stop100.setAttribute("stop-opacity", "0.0");
    
    gradient.appendChild(stop0);
    gradient.appendChild(stop100);
    defs.appendChild(gradient);
    
    // Neon glow filter
    const filter = document.createElementNS(svgNamespace, "filter");
    filter.setAttribute("id", "chart-glow");
    filter.setAttribute("x", "-10%");
    filter.setAttribute("y", "-10%");
    filter.setAttribute("width", "120%");
    filter.setAttribute("height", "120%");
    
    const blur = document.createElementNS(svgNamespace, "feGaussianBlur");
    blur.setAttribute("stdDeviation", "4");
    blur.setAttribute("result", "blur");
    
    const merge = document.createElementNS(svgNamespace, "feMerge");
    const mergeNode1 = document.createElementNS(svgNamespace, "feMergeNode");
    mergeNode1.setAttribute("in", "blur");
    const mergeNode2 = document.createElementNS(svgNamespace, "feMergeNode");
    mergeNode2.setAttribute("in", "SourceGraphic");
    
    merge.appendChild(mergeNode1);
    merge.appendChild(mergeNode2);
    filter.appendChild(blur);
    filter.appendChild(merge);
    defs.appendChild(filter);
    svg.appendChild(defs);
    
    // 2. Draw horizontal grid lines and Y-axis text labels (0%, 25%, 50%, 75%, 100%)
    const yGridValues = [0, 0.25, 0.5, 0.75, 1];
    yGridValues.forEach(val => {
        const y = CHART_MARGIN.top + (1 - val) * chartHeight;
        
        // Grid line
        const gridLine = document.createElementNS(svgNamespace, "line");
        gridLine.setAttribute("x1", CHART_MARGIN.left);
        gridLine.setAttribute("y1", y);
        gridLine.setAttribute("x2", CHART_WIDTH - CHART_MARGIN.right);
        gridLine.setAttribute("y2", y);
        gridLine.setAttribute("class", "chart-grid-line");
        svg.appendChild(gridLine);
        
        // Y-axis label text
        const text = document.createElementNS(svgNamespace, "text");
        text.setAttribute("x", CHART_MARGIN.left - 10);
        text.setAttribute("y", y + 4);
        text.setAttribute("class", "chart-axis-text y-axis");
        text.textContent = `${(val * 100)}%`;
        svg.appendChild(text);
    });
    
    // 3. Draw vertical grid lines and X-axis text labels (5 time marks)
    const numTicks = 5;
    for (let i = 0; i < numTicks; i++) {
        const ratio = i / (numTicks - 1);
        const t = tMin + ratio * (tMax - tMin);
        const x = CHART_MARGIN.left + ratio * chartWidth;
        
        // Vertical axis tick mark (subtle dashed line)
        if (i > 0 && i < numTicks - 1) {
            const vLine = document.createElementNS(svgNamespace, "line");
            vLine.setAttribute("x1", x);
            vLine.setAttribute("y1", CHART_MARGIN.top);
            vLine.setAttribute("x2", x);
            vLine.setAttribute("y2", CHART_HEIGHT - CHART_MARGIN.bottom);
            vLine.setAttribute("class", "chart-grid-line");
            svg.appendChild(vLine);
        }
        
        // X-axis label text
        const text = document.createElementNS(svgNamespace, "text");
        text.setAttribute("x", x);
        text.setAttribute("y", CHART_HEIGHT - CHART_MARGIN.bottom + 20);
        text.setAttribute("class", "chart-axis-text x-axis");
        text.textContent = formatTickTime(t);
        svg.appendChild(text);
    }
    
    // 4. Construct path points string
    let pathD = "";
    projectedPoints.forEach((pt, i) => {
        if (i === 0) {
            pathD += `M ${pt.x} ${pt.y}`;
        } else {
            pathD += ` L ${pt.x} ${pt.y}`;
        }
    });
    
    // Draw area path (gradient filled region)
    if (projectedPoints.length > 0) {
        const areaD = `${pathD} L ${projectedPoints[projectedPoints.length - 1].x} ${CHART_HEIGHT - CHART_MARGIN.bottom} L ${projectedPoints[0].x} ${CHART_HEIGHT - CHART_MARGIN.bottom} Z`;
        const areaPath = document.createElementNS(svgNamespace, "path");
        areaPath.setAttribute("d", areaD);
        areaPath.setAttribute("class", "chart-area-path");
        svg.appendChild(areaPath);
    }
    
    // Draw line path (accent colored stroke line)
    const linePath = document.createElementNS(svgNamespace, "path");
    linePath.setAttribute("d", pathD);
    linePath.setAttribute("class", "chart-line-path");
    linePath.setAttribute("stroke", accentColor);
    linePath.setAttribute("filter", "url(#chart-glow)");
    svg.appendChild(linePath);
    
    // 5. Draw data point markers (circles)
    projectedPoints.forEach(pt => {
        const circle = document.createElementNS(svgNamespace, "circle");
        circle.setAttribute("cx", pt.x);
        circle.setAttribute("cy", pt.y);
        circle.setAttribute("r", "4");
        circle.setAttribute("class", "chart-data-point");
        circle.setAttribute("stroke", accentColor);
        svg.appendChild(circle);
    });
    
    // 6. Append absolute vertical crosshair tracker and floating circle
    const trackerLine = document.createElementNS(svgNamespace, "line");
    trackerLine.setAttribute("y1", CHART_MARGIN.top);
    trackerLine.setAttribute("y2", CHART_HEIGHT - CHART_MARGIN.bottom);
    trackerLine.setAttribute("stroke", "rgba(255, 255, 255, 0.2)");
    trackerLine.setAttribute("stroke-width", "1.5");
    trackerLine.setAttribute("stroke-dasharray", "3 3");
    trackerLine.style.display = "none";
    svg.appendChild(trackerLine);
    
    const trackerDot = document.createElementNS(svgNamespace, "circle");
    trackerDot.setAttribute("r", "5.5");
    trackerDot.setAttribute("stroke", "#141124");
    trackerDot.setAttribute("stroke-width", "2");
    trackerDot.style.display = "none";
    svg.appendChild(trackerDot);
    
    elements.chartContainer.appendChild(svg);
    
    // 7. Inject Tooltip overlay element
    let tooltip = document.getElementById('chart-tooltip');
    if (!tooltip) {
        tooltip = document.createElement('div');
        tooltip.id = 'chart-tooltip';
        tooltip.className = 'chart-tooltip';
        elements.chartContainer.appendChild(tooltip);
    }
    
    // Interactive mouse trackers
    const hideTracker = () => {
        trackerLine.style.display = "none";
        trackerDot.style.display = "none";
        tooltip.style.opacity = "0";
        tooltip.style.transform = "translateY(5px)";
    };
    
    svg.addEventListener('mousemove', (e) => {
        const rect = svg.getBoundingClientRect();
        const mouseX = e.clientX - rect.left;
        
        // Translate client-pixel offset to relative SVG coordinates
        const svgX = (mouseX / rect.width) * CHART_WIDTH;
        
        // Find nearest data point horizontally
        let closestPt = null;
        let minDistance = Infinity;
        
        projectedPoints.forEach(pt => {
            const dist = Math.abs(pt.x - svgX);
            if (dist < minDistance) {
                minDistance = dist;
                closestPt = pt;
            }
        });
        
        // Highlight crosshair details if cursor lies within reasonable distance threshold
        if (closestPt && minDistance < 40) {
            trackerLine.setAttribute('x1', closestPt.x);
            trackerLine.setAttribute('x2', closestPt.x);
            trackerLine.style.display = "block";
            
            trackerDot.setAttribute('cx', closestPt.x);
            trackerDot.setAttribute('cy', closestPt.y);
            
            const ptStatus = getQuotaState(closestPt.value);
            const ptColor = getStatusRGBHex(ptStatus);
            
            trackerDot.setAttribute('fill', ptColor);
            trackerDot.style.display = "block";
            
            // Convert relative SVG placement to percentage coordinates
            const tooltipLeft = (closestPt.x / CHART_WIDTH) * rect.width;
            const tooltipTop = (closestPt.y / CHART_HEIGHT) * rect.height;
            
            tooltip.style.left = `${tooltipLeft + 15}px`;
            tooltip.style.top = `${tooltipTop - 50}px`;
            tooltip.style.borderColor = ptColor;
            tooltip.style.opacity = "1";
            tooltip.style.transform = "translateY(0)";
            
            tooltip.innerHTML = `
                <div class="chart-tooltip-time">${formatTooltipTime(closestPt.timestamp)}</div>
                <div class="chart-tooltip-value">
                    <span class="chart-tooltip-marker" style="background: ${ptColor}"></span>
                    Remaining: ${(closestPt.value * 100).toFixed(2)}%
                </div>
            `;
        } else {
            hideTracker();
        }
    });
    
    svg.addEventListener('mouseleave', hideTracker);
}

// Helpers
function getQuotaState(fraction) {
    if (fraction <= 0.20) return 'danger';
    if (fraction <= 0.50) return 'warning';
    return 'safe';
}

// Get status color in hex
function getStatusRGBHex(status) {
    if (status === 'danger') return '#ff007f'; // Neon magenta
    if (status === 'warning') return '#f1c40f'; // Amber
    return '#00f2fe'; // Neon Teal
}

function sanitizeId(str) {
    return str.replace(/[^a-zA-Z0-9-_]/g, '_');
}

function formatTimestamp(isoString) {
    if (!isoString) return 'Never';
    try {
        const date = new Date(isoString);
        if (isNaN(date.getTime())) return 'Never';
        return date.toLocaleString(undefined, {
            month: 'short',
            day: 'numeric',
            hour: '2-digit',
            minute: '2-digit'
        });
    } catch (e) {
        return 'Never';
    }
}

function formatTickTime(timestampMs) {
    const d = new Date(timestampMs);
    const h = String(d.getHours()).padStart(2, '0');
    const m = String(d.getMinutes()).padStart(2, '0');
    return `${h}:${m}`;
}

function formatTooltipTime(timestampMs) {
    const d = new Date(timestampMs);
    return d.toLocaleDateString(undefined, {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
    });
}

// Run Timers every second
function startCountdownTicks() {
    setInterval(() => {
        tickCountdowns();
    }, 1000);
}

function tickCountdowns() {
    if (state.activeTimers.length === 0) return;
    
    const now = Date.now();
    
    state.activeTimers.forEach(timer => {
        const elementId = `countdown-${sanitizeId(timer.name)}`;
        const el = document.getElementById(elementId);
        if (!el) return;
        
        const diffMs = timer.targetTime - now;
        
        if (diffMs <= 0) {
            el.textContent = '00:00:00';
            el.classList.add('expired');
            return;
        }
        
        const diffSecs = Math.floor(diffMs / 1000);
        const hours = Math.floor(diffSecs / 3600);
        const minutes = Math.floor((diffSecs % 3600) / 60);
        const seconds = diffSecs % 60;
        
        const pad = (num) => String(num).padStart(2, '0');
        
        if (hours > 24) {
            const days = Math.floor(hours / 24);
            el.textContent = `${days}d ${pad(hours % 24)}h ${pad(minutes)}m`;
        } else {
            el.textContent = `${pad(hours)}:${pad(minutes)}:${pad(seconds)}`;
        }
    });
}

// Boot application
document.addEventListener('DOMContentLoaded', init);
