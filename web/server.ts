import { serve } from 'bun';
import { file } from 'bun';

// Connection status tracking
let gotelConnectionStatus = {
  connected: false,
  lastChecked: Date.now(),
  consecutiveFailures: 0
};

// Test connection to GoTel server and return mock data with notices when unavailable
const testGotelConnection = async () => {
  try {
    const response = await fetch('http://localhost:3200/api/services', {
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
      return true;
    } else {
      gotelConnectionStatus.connected = false;
      gotelConnectionStatus.consecutiveFailures++;
      gotelConnectionStatus.lastChecked = Date.now();
      console.log(`❌ GoTel server responded with status: ${response.status}`);
      return false;
    }
  } catch (error) {
    gotelConnectionStatus.connected = false;
    gotelConnectionStatus.consecutiveFailures++;
    gotelConnectionStatus.lastChecked = Date.now();
    console.log('❌ Failed to connect to GoTel server:', (error as Error).message);
    return false;
  }
};

// Get traces data with connection notice
const getTracesData = async () => {
  try {
    const response = await fetch('http://localhost:3200/api/traces', {
      method: 'GET',
      headers: { 'Accept': 'application/json' }
    });
    
    if (response.ok) {
      gotelConnectionStatus.connected = true;
      gotelConnectionStatus.consecutiveFailures = 0;
      gotelConnectionStatus.lastChecked = Date.now();
      return await response.json();
    } else {
      gotelConnectionStatus.connected = false;
      gotelConnectionStatus.consecutiveFailures++;
      gotelConnectionStatus.lastChecked = Date.now();
      
      return [
        {
          trace_id: 'connection-notice',
          span_name: 'GoTel Server Connection Issue',
          service_name: 'system',
          duration_ms: 0,
          status_code: 3, // Custom status for notices
          span_count: 1,
          notice: true,
          message: `❌ Cannot connect to GoTel server (attempt ${gotelConnectionStatus.consecutiveFailures})`
        }
      ];
    }
  } catch (error) {
    gotelConnectionStatus.connected = false;
    gotelConnectionStatus.consecutiveFailures++;
    gotelConnectionStatus.lastChecked = Date.now();
    
    return [
      {
        trace_id: 'connection-notice',
        span_name: 'GoTel Server Error',
        service_name: 'system', 
        duration_ms: 0,
        status_code: 3,
        span_count: 1,
        notice: true,
        message: `❌ Network error: ${(error as Error).message}`
      }
    ];
  }
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
  
  try {
    const response = await fetch('http://localhost:3200/api/spans?limit=100', {
      method: 'GET', 
      headers: { 'Accept': 'application/json' }
    });
    
    if (response.ok) {
      return await response.json();
    } else {
      gotelConnectionStatus.connected = false;
      gotelConnectionStatus.consecutiveFailures++;
      gotelConnectionStatus.lastChecked = Date.now();
      
      return [
        {
          trace_id: 'connection-notice',
          span_id: 'system-notice',
          parent_span_id: null,
          service_name: 'system',
          span_name: 'GoTel Server Unavailable',
          start_time: Date.now() * 1000000,
          end_time: Date.now() * 1000000 + 50000000,
          duration_ms: 50,
          status_code: 3,
          status_message: `Server responded with status ${response.status}`,
          notice: true
        }
      ];
    }
  } catch (error) {
    gotelConnectionStatus.connected = false;
    gotelConnectionStatus.consecutiveFailures++;
    gotelConnectionStatus.lastChecked = Date.now();
    
    return [
      {
        trace_id: 'connection-notice',
        span_id: 'system-notice', 
        parent_span_id: null,
        service_name: 'system',
        span_name: 'Network Connection Error',
        start_time: Date.now() * 1000000,
        end_time: Date.now() * 1000000 + 75000000,
        duration_ms: 75,
        status_code: 3,
        status_message: (error as Error).message,
        notice: true
      }
    ];
  }
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
  
  try {
    const response = await fetch('http://localhost:3200/api/exceptions', {
      method: 'GET',
      headers: { 'Accept': 'application/json' }
    });
    
    if (response.ok) {
      return await response.json();
    } else {
      gotelConnectionStatus.connected = false;
      gotelConnectionStatus.consecutiveFailures++;
      gotelConnectionStatus.lastChecked = Date.now();
      
      return [
        {
          trace_id: 'connection-notice',
          span_id: 'system-exception',
          service_name: 'system',
          span_name: 'API Server Error',
          exception_type: 'ServerError',
          message: `GoTel server responded with status ${response.status}`,
          timestamp: Date.now(),
          severity: 'critical',
          notice: true
        }
      ];
    }
  } catch (error) {
    return [
      {
        trace_id: 'connection-notice',
        span_id: 'system-exception',
        service_name: 'system',
        span_name: 'Network Exception',
        exception_type: 'NetworkError',
        message: (error as Error).message,
        timestamp: Date.now(),
        severity: 'critical',
        notice: true
      }
    ];
  }
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
  
  try {
    const response = await fetch('http://localhost:3200/api/services', {
      method: 'GET',
      headers: { 'Accept': 'application/json' }
    });
    
    if (response.ok) {
      const services = await response.json();
      return services.map((service: any) => ({ ...service, status: 'online' }));
    } else {
      gotelConnectionStatus.connected = false;
      gotelConnectionStatus.consecutiveFailures++;
      gotelConnectionStatus.lastChecked = Date.now();
      
      return [
        { 
          name: 'GoTel Server', 
          version: 'Server Error',
          status: 'error'
        }
      ];
    }
  } catch (error) {
    return [
      { 
        name: 'GoTel Server', 
        version: 'Network Error',
        status: 'offline'
      }
    ];
  }
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