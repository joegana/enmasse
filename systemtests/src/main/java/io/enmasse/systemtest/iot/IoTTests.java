/*
 * Copyright 2020, EnMasse authors.
 * License: Apache License 2.0 (see the file LICENSE or http://apache.org/licenses/LICENSE-2.0.html).
 */

package io.enmasse.systemtest.iot;

import static io.enmasse.systemtest.bases.iot.ITestIoTBase.IOT_PROJECT_NAMESPACE;

import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayNameGeneration;
import org.junit.jupiter.api.TestInstance;
import org.junit.jupiter.api.extension.ExtendWith;

import io.enmasse.systemtest.IndicativeSentences;
import io.enmasse.systemtest.bases.ITestSeparator;
import io.enmasse.systemtest.bases.JUnitWorkaround;
import io.enmasse.systemtest.listener.JunitCallbackListener;
import io.enmasse.systemtest.platform.Kubernetes;

/**
 * Marker interface for IoT tests, which provision their own IoT infrastructure.
 */
@ExtendWith(JunitCallbackListener.class)
@ExtendWith(JUnitWorkaround.class)
@DisplayNameGeneration(IndicativeSentences.class)
@TestInstance(TestInstance.Lifecycle.PER_CLASS)
public interface IoTTests extends ITestSeparator {

    @BeforeAll
    public static void deployDefaultCerts() throws Exception {
        IoTTestSession.deployDefaultCerts();
    }

    @BeforeEach
    default void createNamespace() {
        Kubernetes.getInstance().createNamespace(IOT_PROJECT_NAMESPACE);
    }

}
