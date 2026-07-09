// State Management
let state = {
    apiKey: localStorage.getItem('agentic_quota_api_key') || '',
    quotas: {},
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
    autoRefreshSelect: document.getElementById('auto-refresh-select')
};

// SVG Settings for Circle Gauge
const SVG_RADIUS = 50;
const SVG_CIRCUMFERENCE = 2 * Math.PI * SVG_RADIUS; // ~314.16

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
        fetchQuotaData();
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
    elements.refreshBtn.addEventListener('click', () => fetchQuotaData(true));
    
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
        fetchQuotaData();
        setupAutoRefresh();
    });
    
    elements.autoRefreshSelect.addEventListener('change', (e) => {
        const value = parseInt(e.target.value);
        state.autoRefreshInterval = value;
        localStorage.setItem('agentic_quota_refresh', value);
        setupAutoRefresh();
    });
}

// Modal Controls
function showModal() {
    elements.settingsModal.classList.remove('hidden');
    elements.apiKeyInput.focus();
}

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
            fetchQuotaData();
        }, state.autoRefreshInterval * 1000);
    }
}

// Fetch Data from Server
async function fetchQuotaData(isManual = false) {
    if (state.isFetching) return;
    state.isFetching = true;
    
    // Add spinning animation to refresh icon
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
            // Success call, but empty database
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

// Process and parse metrics
function processQuotaData(quotaMap) {
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
    
    // Compute target reset times based on client clock to prevent drift
    const now = Date.now();
    state.activeTimers = [];
    
    const sortedKeys = keys.sort();
    let gridHTML = '';
    
    sortedKeys.forEach(name => {
        const q = quotaMap[name];
        
        // Calculate stable local target time: Date.now() + reset_in_seconds * 1000
        const targetTime = now + (q.reset_in_seconds * 1000);
        
        state.activeTimers.push({
            name: name,
            targetTime: targetTime
        });
        
        const cardState = getQuotaState(q.remaining_fraction);
        const percentText = (q.remaining_fraction * 100).toFixed(1) + '%';
        const formattedReset = formatTimestamp(q.reset_time);
        
        // Circular progress calculations
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
    
    // Do a tick immediately to populate countdowns
    tickCountdowns();
}

// Helpers
function getQuotaState(fraction) {
    if (fraction <= 0.20) return 'danger';
    if (fraction <= 0.50) return 'warning';
    return 'safe';
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
