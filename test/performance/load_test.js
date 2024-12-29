import http from 'k6/http';
import { check, sleep } from 'k6';

export let options = {
    stages: [
        { duration: '30s', target: 20 }, // Ramp up to 20 users
        { duration: '1m', target: 20 },  // Stay at 20 users
        { duration: '30s', target: 0 },  // Ramp down to 0 users
    ],
    thresholds: {
        http_req_duration: ['p(95)<500'], // 95% of requests should be below 500ms
        http_req_failed: ['rate<0.01'],   // Less than 1% of requests should fail
    },
};

const BASE_URL = 'http://localhost:9090';

export default function () {
    // Health check
    let healthCheck = http.get(`${BASE_URL}/health`);
    check(healthCheck, {
        'health check status is 200': (r) => r.status === 200,
        'health check response is healthy': (r) => r.json().status === true,
    });

    // Metrics check
    let metrics = http.get(`${BASE_URL}/metrics`);
    check(metrics, {
        'metrics status is 200': (r) => r.status === 200,
    });

    // Upload test (with small file)
    let testFile = open('./test.jpg', 'b');
    let uploadData = {
        file: http.file(testFile, 'test.jpg'),
        bucket: 'test-bucket',
    };

    let upload = http.post(`${BASE_URL}/upload`, uploadData);
    check(upload, {
        'upload status is 200': (r) => r.status === 200,
    });

    sleep(1);
} 