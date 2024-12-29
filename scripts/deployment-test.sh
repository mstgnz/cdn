#!/bin/bash

# Test Blue/Green Deployment
test_blue_green() {
    echo "Testing Blue/Green Deployment..."
    
    # Deploy blue version
    kubectl apply -f k8s/blue-green/deployment-blue.yaml
    kubectl apply -f k8s/blue-green/service.yaml
    
    # Wait for blue deployment
    kubectl rollout status deployment/cdn-blue
    
    # Test blue version
    curl -f http://localhost/health || exit 1
    
    # Deploy green version
    kubectl apply -f k8s/blue-green/deployment-green.yaml
    
    # Wait for green deployment
    kubectl rollout status deployment/cdn-green
    
    # Switch to green
    kubectl patch service cdn-service -p '{"spec":{"selector":{"version":"green"}}}'
    
    # Test green version
    sleep 10
    curl -f http://localhost/health || exit 1
    
    echo "Blue/Green Deployment test successful!"
}

# Test Multi-Region Deployment
test_multi_region() {
    echo "Testing Multi-Region Deployment..."
    
    regions=("us-west1" "eu-west1" "asia-east1")
    
    for region in "${regions[@]}"; do
        # Switch context to region
        kubectl config use-context cdn-${region}
        
        # Apply regional config
        kubectl apply -f k8s/multi-region/config-map-${region}.yaml
        kubectl apply -f k8s/deployment.yaml
        
        # Wait for deployment
        kubectl rollout status deployment/cdn-service
        
        # Test regional endpoint
        curl -f http://${region}.cdn.example.com/health || exit 1
    done
    
    echo "Multi-Region Deployment test successful!"
}

# Test Disaster Recovery
test_disaster_recovery() {
    echo "Testing Disaster Recovery..."
    
    # Create test data
    echo "test-file" > test.txt
    kubectl exec -it $(kubectl get pod -l app=minio -o jsonpath='{.items[0].metadata.name}') -- mc cp test.txt minio/cdn-bucket/
    
    # Trigger backup
    kubectl create job --from=cronjob/cdn-backup manual-backup
    
    # Wait for backup completion
    kubectl wait --for=condition=complete job/manual-backup
    
    # Simulate disaster
    kubectl delete deployment cdn-service
    kubectl delete pvc --all
    
    # Restore from backup
    kubectl apply -f k8s/disaster-recovery/restore-job.yaml
    
    # Wait for restore completion
    kubectl wait --for=condition=complete job/cdn-restore
    
    # Verify restore
    kubectl exec -it $(kubectl get pod -l app=minio -o jsonpath='{.items[0].metadata.name}') -- mc cat minio/cdn-bucket/test.txt | grep "test-file" || exit 1
    
    echo "Disaster Recovery test successful!"
}

# Run all tests
echo "Starting deployment tests..."
test_blue_green
test_multi_region
test_disaster_recovery
echo "All deployment tests completed successfully!" 