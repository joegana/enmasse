/*
 * Copyright 2017 Red Hat Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package enmasse.address.controller.model;

/**
 * Represents a tenant
 */
public final class TenantId {
    private final String idString;

    private TenantId(String idString) {
        this.idString = idString;
    }

    @Override
    public String toString() {
        return idString;
    }

    public static TenantId fromString(String idString) {
        return new TenantId(idString);
    }

    @Override
    public boolean equals(Object o) {
        if (this == o) return true;
        if (o == null || getClass() != o.getClass()) return false;

        TenantId tenantId = (TenantId) o;

        return idString.equals(tenantId.idString);
    }

    @Override
    public int hashCode() {
        return idString.hashCode();
    }
}
