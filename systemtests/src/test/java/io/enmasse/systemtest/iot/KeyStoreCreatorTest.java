/*
 * Copyright 2020, EnMasse authors.
 * License: Apache License 2.0 (see the file LICENSE or http://apache.org/licenses/LICENSE-2.0.html).
 */

package io.enmasse.systemtest.iot;

import static org.junit.Assert.assertNotNull;

import java.time.Duration;
import java.time.Instant;

import org.bouncycastle.asn1.x500.X500Name;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.EnumSource;

import io.enmasse.systemtest.TestTag;
import io.enmasse.systemtest.iot.DeviceCertificateManager.Mode;

@Tag(TestTag.FRAMEWORK)
public class KeyStoreCreatorTest {

    @Test
    public void testBasicRsa() throws Exception {
        var mgr = new DeviceCertificateManager(Mode.RSA, new X500Name("O=EnMasse,C=IO"));
        var device = mgr.createDevice("foo", Instant.now(), Duration.ofDays(90));
        KeyStoreCreator.from(device.getKey().getPrivate(), device.getCertificate());
    }

    @Test
    public void testBasicEc() throws Exception {
        var mgr = new DeviceCertificateManager(Mode.EC, new X500Name("O=EnMasse,C=IO"));
        var device = mgr.createDevice("foo", Instant.now(), Duration.ofDays(90));
        KeyStoreCreator.from(device.getKey().getPrivate(), device.getCertificate());
    }

    @ParameterizedTest(name = "testToByteArray-{0}")
    @EnumSource(Mode.class)
    public void testToByteArray(final Mode mode) throws Exception {
        var mgr = new DeviceCertificateManager(mode, new X500Name("O=EnMasse,C=IO"));
        var device = mgr.createDevice("foo", Instant.now(), Duration.ofDays(90));

        byte[] bytes = KeyStoreCreator.toByteArray(device.getKey().getPrivate(), device.getCertificate());

        assertNotNull(bytes);
    }

}
