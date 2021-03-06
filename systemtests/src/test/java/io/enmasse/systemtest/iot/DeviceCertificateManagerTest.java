/*
 * Copyright 2020, EnMasse authors.
 * License: Apache License 2.0 (see the file LICENSE or http://apache.org/licenses/LICENSE-2.0.html).
 */

package io.enmasse.systemtest.iot;

import static io.enmasse.systemtest.TestTag.FRAMEWORK;
import static org.junit.Assert.assertNotNull;

import java.security.cert.X509Certificate;
import java.time.Duration;
import java.time.Instant;

import org.bouncycastle.asn1.x500.X500Name;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;

import io.enmasse.systemtest.iot.DeviceCertificateManager.Device;
import io.enmasse.systemtest.iot.DeviceCertificateManager.Mode;

@Tag(FRAMEWORK)
public class DeviceCertificateManagerTest {

    @Test
    public void testRsa() throws Exception {
        final DeviceCertificateManager mgr = new DeviceCertificateManager(Mode.RSA, new X500Name("CN=foo,OU=bar,O=baz"));

        final X509Certificate cert = mgr.getCertificate();
        assertNotNull(cert);

        final Device device = mgr.createDevice("dev1", Instant.now(), Duration.ofDays(90));
        assertNotNull(device);
        assertNotNull(device.getKey());
        assertNotNull(device.getCertificate());
    }

    @Test
    public void testEc() throws Exception {
        final DeviceCertificateManager mgr = new DeviceCertificateManager(Mode.EC, new X500Name("CN=foo,OU=bar,O=baz"));

        final X509Certificate cert = mgr.getCertificate();
        assertNotNull(cert);

        final Device device = mgr.createDevice("dev1", Instant.now(), Duration.ofDays(90));
        assertNotNull(device);
        assertNotNull(device.getKey());
        assertNotNull(device.getCertificate());
    }

}
