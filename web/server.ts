import { serve } from 'bun';
import { file } from 'bun';

// Connection status tracking
let gotelConnectionStatus = {
  connected: false,
  lastChecked: Date.now(),
  consecutiveFailures: 0
};

// Test connection to GoTel server and return mock data with notices when unavailable
const fetchFromUpstream = async (endpoint: string, errorFactory: (msg: string, isNetworkError: boolean) => any) => {
  try {
    const response = await fetch(`http://localhost:3200${endpoint}`, {
      method: 'GET',
      headers: { 'Accept': 'application/json' }
    });
    
    if (response.ok) {
      if (!gotelConnectionStatus.connected) {
        gotelConnectionStatus.connected = true;
        gotelConnectionStatus.consecutiveFailures = 0;
        gotelConnectionStatus.lastChecked = Date.now();
        console.log('✅ Reconnected to GoTel server successfully!');
      }
      return await response.json();
    } else {
      gotelConnectionStatus.connected = false;
      gotelConnectionStatus.consecutiveFailures++;
      gotelConnectionStatus.lastChecked = Date.now();
      // Only log on first failure or periodically to avoid spam
      if (gotelConnectionStatus.consecutiveFailures === 1 || gotelConnectionStatus.consecutiveFailures % 10 === 0) {
        console.log(`❌ GoTel server responded with status: ${response.status}`);
      }
      return errorFactory(`Server responded with status ${response.status}`, false);
    }
  } catch (error) {
    gotelConnectionStatus.connected = false;
    gotelConnectionStatus.consecutiveFailures++;
    gotelConnectionStatus.lastChecked = Date.now();
    // Only log on first failure or periodically
    if (gotelConnectionStatus.consecutiveFailures === 1 || gotelConnectionStatus.consecutiveFailures % 10 === 0) {
      console.log('❌ Failed to connect to GoTel server:', (error as Error).message);
    }
    return errorFactory((error as Error).message, true);
  }
};

const testGotelConnection = async () => {
  return await fetchFromUpstream('/api/services', () => false) !== false;
};

// Get traces data with connection notice
const getTracesData = async () => {
  return await fetchFromUpstream('/api/traces', (msg, isNetwork) => [{
    trace_id: 'connection-notice',
    span_name: isNetwork ? 'GoTel Server Error' : 'GoTel Server Connection Issue',
    service_name: 'system',
    duration_ms: 0,
    status_code: 3, // Custom status for notices
    span_count: 1,
    notice: true,
    message: isNetwork ? `❌ Network error: ${msg}` : `❌ Cannot connect to GoTel server (attempt ${gotelConnectionStatus.consecutiveFailures})`
  }]);
};

const getSpansData = async () => {
  if (!gotelConnectionStatus.connected) {
    return [
      {
        trace_id: 'connection-notice',
        span_id: 'system-notice',
        parent_span_id: null,
        service_name: 'system',
        span_name: 'No GoTel Connection',
        start_time: Date.now() * 1000000,
        end_time: Date.now() * 1000000 + 100000000,
        duration_ms: 100,
        status_code: 3,
        status_message: 'Waiting for GoTel server connection',
        notice: true
      }
    ];
  }
  
  return await fetchFromUpstream('/api/spans?limit=100', (msg, isNetwork) => [{
    trace_id: 'connection-notice',
    span_id: 'system-notice',
    parent_span_id: null,
    service_name: 'system',
    span_name: isNetwork ? 'Network Connection Error' : 'GoTel Server Unavailable',
    start_time: Date.now() * 1000000,
    end_time: Date.now() * 1000000 + 50000000,
    duration_ms: 50,
    status_code: 3,
    status_message: msg,
    notice: true
  }]);
};

const getExceptionsData = async () => {
  if (!gotelConnectionStatus.connected) {
    return [
      {
        trace_id: 'connection-notice',
        span_id: 'system-exception',
        service_name: 'system',
        span_name: 'Connection System Notice',
        exception_type: 'ConnectionError',
        message: `Unable to connect to GoTel server (${gotelConnectionStatus.consecutiveFailures} attempts)`,
        timestamp: Date.now(),
        severity: 'warning',
        notice: true,
        details: 'Check if GoTel server is running on port 3200'
      }
    ];
  }
  
  return await fetchFromUpstream('/api/exceptions', (msg, isNetwork) => [{
    trace_id: 'connection-notice',
    span_id: 'system-exception',
    service_name: 'system',
    span_name: isNetwork ? 'Network Exception' : 'API Server Error',
    exception_type: isNetwork ? 'NetworkError' : 'ServerError',
    message: msg,
    timestamp: Date.now(),
    severity: 'critical',
    notice: true
  }]);
};

const getServicesData = async () => {
  if (!gotelConnectionStatus.connected) {
    return [
      { 
        name: 'GoTel Server (UNAVAILABLE)', 
        version: `Connection Failed (${gotelConnectionStatus.consecutiveFailures} attempts)`,
        status: 'offline'
      }
    ];
  }
  
  const result = await fetchFromUpstream('/api/services', (msg, isNetwork) => [{
    name: 'GoTel Server',
    version: isNetwork ? 'Network Error' : 'Server Error',
    status: isNetwork ? 'offline' : 'error'
  }]);

  // If result is an array and looks like services (check first item), add status
  if (Array.isArray(result) && result.length > 0 && result[0].name !== 'GoTel Server') {
    return result.map((service: any) => ({ ...service, status: 'online' }));
  }
  return result;
};

// Get port from environment variable or use default
const PORT = parseInt(process.env.PORT || '3000');

const server = serve({
  port: PORT,
  hostname: '0.0.0.0', // All interfaces
  async fetch(req) {
    const url = new URL(req.url);
    
    // CORS headers for development
    const corsHeaders = {
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'GET, POST, PUT, DELETE, OPTIONS',
      'Access-Control-Allow-Headers': 'Content-Type, Authorization',
    };
    
    if (req.method === 'OPTIONS') {
      return new Response(null, { headers: corsHeaders });
    }
    
    // Serve main HTML
    if (url.pathname === '/' || url.pathname === '/index.html') {
      return new Response(file('./index.html'), {
        headers: { ...corsHeaders, 'Content-Type': 'text/html' }
      });
    }
    
    // Serve bundled CSS file
    if (url.pathname === '/dist/bundle.css') {
      return new Response(file('./dist/bundle.css'), {
        headers: { ...corsHeaders, 'Content-Type': 'text/css' }
      });
    }
    
    // Serve PerfCascade CSS
    if (url.pathname === '/dist/perf-cascade.css') {
      return new Response(file('./node_modules/perf-cascade/dist/perf-cascade.css'), {
        headers: { ...corsHeaders, 'Content-Type': 'text/css' }
      });
    }
    
    // Serve PerfCascade JS
    if (url.pathname === '/dist/perf-cascade.min.js') {
      return new Response(file('./node_modules/perf-cascade/dist/perf-cascade.min.js'), {
        headers: { ...corsHeaders, 'Content-Type': 'application/javascript' }
      });
    }

    // Serve built JavaScript files
    if (url.pathname.endsWith('.js')) {
      return new Response(file(`dist${url.pathname}`), {
        headers: { ...corsHeaders, 'Content-Type': 'application/javascript' }
      });
    }
    
    // API endpoint for traces summary
    if (url.pathname === '/api/traces') {
      const traces = await getTracesData();
      
      return new Response(JSON.stringify(traces), {
        headers: { ...corsHeaders, 'Content-Type': 'application/json' }
      });
    }
    
    // API endpoint for individual spans
    if (url.pathname === '/api/spans') {
      const spans = await getSpansData();
      
      return new Response(JSON.stringify(spans), {
        headers: { ...corsHeaders, 'Content-Type': 'application/json' }
      });
    }
    
    // API endpoint for exceptions
    if (url.pathname === '/api/exceptions') {
      const exceptions = await getExceptionsData();
      
      return new Response(JSON.stringify(exceptions), {
        headers: { ...corsHeaders, 'Content-Type': 'application/json' }
      });
    }
    
    // API endpoint for services
    if (url.pathname === '/api/services') {
      const services = await getServicesData();
      
      return new Response(JSON.stringify(services), {
        headers: { ...corsHeaders, 'Content-Type': 'application/json' }
      });
    }
    
    return new Response('Not found', { 
      status: 404,
      headers: corsHeaders
    });
  }
});

console.log(`GoTel Web UI running on http://${server.hostname}:${PORT}`);
console.log(`API endpoints available:`);
console.log(`  GET /api/traces - List traces`);
console.log(`  GET /api/spans - List spans`);
console.log(`  GET /api/exceptions - List exceptions`);
console.log(`  GET /api/services - List services`);
console.log(`\n🔍 Testing connection to GoTel server at http://localhost:3200...`);

// Test connection status periodically
setInterval(async () => {
  await testGotelConnection();
}, 5000); // Check every 5 seconds