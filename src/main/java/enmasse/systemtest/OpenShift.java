package enmasse.systemtest;

import io.fabric8.kubernetes.api.model.Pod;
import io.fabric8.kubernetes.api.model.Service;
import io.fabric8.kubernetes.api.model.ServicePort;
import io.fabric8.kubernetes.client.Config;
import io.fabric8.kubernetes.client.ConfigBuilder;
import io.fabric8.openshift.api.model.Route;
import io.fabric8.openshift.client.DefaultOpenShiftClient;
import io.fabric8.openshift.client.OpenShiftClient;

import java.util.List;
import java.util.Map;
import java.util.stream.Collectors;

/**
 * Handles interaction with openshift cluster
 */
public class OpenShift {
    private final Environment environment;
    private final OpenShiftClient client;
    private final String namespace;

    public OpenShift(Environment environment, String namespace) {
        this.environment = environment;
        Config config = new ConfigBuilder()
                .withMasterUrl(environment.openShiftUrl())
                .withOauthToken(environment.openShiftToken())
                .withUsername(environment.openShiftUser())
                .build();
        client = new DefaultOpenShiftClient(config);
        this.namespace = namespace;
    }

    public OpenShiftClient getClient() {
        return client;
    }

    public Endpoint getEndpoint(String serviceName, String port) {
        Service service = client.services().inNamespace(namespace).withName(serviceName).get();
        return new Endpoint(service.getSpec().getClusterIP(), getPort(service, port));
    }

    public Endpoint getSecureEndpoint() {
        return getEndpoint("messaging", "amqps");
    }

    public Endpoint getInsecureEndpoint() {
        return getEndpoint("messaging", "amqp");
    }

    private static int getPort(Service service, String portName) {
        List<ServicePort> ports = service.getSpec().getPorts();
        for (ServicePort port : ports) {
            if (port.getName().equals(portName)) {
                return port.getPort();
            }
        }
        throw new IllegalArgumentException("Unable to find port " + portName + " for service " + service.getMetadata().getName());
    }

    public Endpoint getRestEndpoint() {
        Route route = client.routes().inNamespace(environment.namespace()).withName("restapi").get();
        Endpoint endpoint = new Endpoint(route.getSpec().getHost(), 80);
        if (TestUtils.resolvable(endpoint)) {
            return endpoint;
        } else {
            return new Endpoint("localhost", 80);
        }
    }

    public void setDeploymentReplicas(String name, int numReplicas) {
        client.extensions().deployments()
                .inNamespace(namespace)
                .withName(name)
                .scale(numReplicas, true);
    }

    public List<Pod> listPods() {
        return client.pods()
                .inNamespace(namespace)
                .list()
                .getItems().stream()
                .collect(Collectors.toList());
    }

    public List<Pod> listPods(Map<String, String> labelSelector) {
        return client.pods()
                .inNamespace(namespace)
                .withLabels(labelSelector)
                .list()
                .getItems().stream()
                    .collect(Collectors.toList());
    }

    // Heuristic: if restapi service exists, we are running with a full template
    public boolean isFullTemplate() {
        Service service = client.services().inNamespace(namespace).withName("admin").get();
        return service == null;
    }

    public int getExpectedPods() {
        if (isFullTemplate()) {
            return 8; // configserv, address-controller, queue-scheduler, ragent, qdrouterd, subscription, mqtt gateway, mqtt lwt
        } else {
            if (environment.isMultitenant()) {
                return 5; // admin, qdrouterd, subscription, mqtt gateway, mqtt lwt
            } else {
                return 6; // address-controller, admin, qdrouterd, subscription, mqtt gateway, mqtt lwt
            }
        }
    }

    public Endpoint getRouteEndpoint(String routeName) {
        Route route = client.routes().inNamespace(namespace).withName(routeName).get();
        return new Endpoint(route.getSpec().getHost(), 443);
    }
}
