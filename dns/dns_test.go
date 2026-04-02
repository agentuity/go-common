package dns

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/agentuity/go-common/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestDNSIsValidAndCached(t *testing.T) {
	c := cache.NewInMemory(context.Background(), cache.WithExpires(time.Second))
	defer c.Close()
	d := NewResolver(WithCache(c))
	ok, ip, err := d.Lookup(context.Background(), "app.agentuity.com")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, ip)
	ok, count := c.Hits("dns:app.agentuity.com")
	assert.True(t, ok)
	assert.Equal(t, 0, count)

	ok, ip, err = d.Lookup(context.Background(), "app.agentuity.com")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, ip)
	ok, count = c.Hits("dns:app.agentuity.com")
	assert.True(t, ok)
	assert.Equal(t, 1, count)
}

func TestGoogleMetadata(t *testing.T) {
	d := NewResolver()
	ok, ip, err := d.Lookup(context.Background(), googleMetadataHostname)
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, ip)
	assert.Equal(t, magicIpAddress, ip.String())
}

func TestFQDN(t *testing.T) {
	assert.Equal(t, formatFQDN("foo.bar.com"), "foo.bar.com")
	assert.Equal(t, formatFQDN("foo"), "foo.agentuity.internal")
	assert.Equal(t, formatFQDN("foo.agentuity.internal"), "foo.agentuity.internal")

	oldCloudId := cloudId
	newCloudId := "new-cloud-id"
	cloudId = newCloudId
	defer func() {
		cloudId = oldCloudId
	}()
	assert.Equal(t, formatFQDN("foo"), "foo-new-cloud-id.agentuity.internal")
	assert.Equal(t, formatFQDN("foo-new-cloud-id"), "foo-new-cloud-id.agentuity.internal")
}

func TestDNSDefault(t *testing.T) {
	ok, ip, err := DefaultDNS.Lookup(context.Background(), "app.agentuity.com")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, ip)
}

func TestDNSDomainIsInvalid(t *testing.T) {
	d := NewResolver()
	ok, ip, err := d.Lookup(context.Background(), "adasf123sdasdxc.dsadasdagentuity.com")
	assert.Error(t, err, "dns lookup failed: Non-Existent Domain")
	assert.False(t, ok)
	assert.Nil(t, ip)
}

func TestDNSLocalHost(t *testing.T) {
	d := NewResolver()
	ok, ip, err := d.Lookup(context.Background(), "localhost")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, ip)

	ok, ip, err = d.Lookup(context.Background(), "localhost")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, ip)
}

func TestDNS127(t *testing.T) {
	c := cache.NewInMemory(context.Background(), cache.WithExpires(time.Second))
	defer c.Close()
	d := NewResolver(WithCache(c))
	ok, ip, err := d.Lookup(context.Background(), "127.0.0.1")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, ip)

	ok, ip, err = d.Lookup(context.Background(), "127.0.0.1")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, ip)
}

func TestDNSIPAddressSkipped(t *testing.T) {
	d := NewResolver()
	ok, ip, err := d.Lookup(context.Background(), "81.0.0.1")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, ip)

	ok, ip, err = d.Lookup(context.Background(), "81.0.0.1")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, ip)
}

func TestDNSPrivateIPSkipped(t *testing.T) {
	c := cache.NewInMemory(context.Background(), cache.WithExpires(time.Second))
	defer c.Close()
	d := NewResolver()
	ok, ip, err := d.Lookup(context.Background(), "10.8.0.1")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, ip)
	ok, count := c.Hits("dns:81.0.0.1")
	assert.False(t, ok)
	assert.Equal(t, 0, count)

	ok, ip, err = d.Lookup(context.Background(), "81.0.0.1")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, ip)
	ok, count = c.Hits("dns:81.0.0.1")
	assert.False(t, ok)
	assert.Equal(t, 0, count)
}

func TestDNSTest(t *testing.T) {
	c := cache.NewInMemory(context.Background(), cache.WithExpires(time.Second))
	defer c.Close()
	d := NewResolver(WithFailIfLocal())
	ok, ip, err := d.Lookup(context.Background(), "customer1.app.localhost.my.company.127.0.0.1.nip.io")
	assert.Error(t, err, ErrInvalidIP)
	assert.False(t, ok)
	assert.Nil(t, ip)
}

func TestInvalidDNSEntries(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
	}{
		{"EmptyHostname", ""},
		{"InvalidCharacters", "invalid!hostname"},
		{"TooLongHostname", "this.is.a.very.long.hostname.that.exceeds.the.maximum.length.allowed.by.the.dns.specification.and.should.therefore.fail.validation"},
		{"HostnameWithSpaces", "hostname with spaces"},
		{"HostnameWithUnderscore", "hostname_with_underscore"},
		{"Unresolved DNS", "bugbounty.dod.network"},
		{"Invalid Hostname", "0xA9.0xFE.0xA9.0xFE"},
		{"Invalid IP Address", "169.254.169.254"},
		{"Invalid IP Address From DNS", "169.254.169.254.nip.io"},
		{"Local IP v6", "[::1]"},
		{"Invalid IP v6", "[::ffff:7f00:1]"},
		{"Invalid Virtual DNS", "localtest.me"},
		{"Invalid Virtual DNS to Private", "spoofed.burpcollaborator.net"},
		{"Docker Host Internal", "host.docker.internal"},
	}

	c := cache.NewInMemory(context.Background(), cache.WithExpires(time.Second))
	defer c.Close()
	d := NewResolver()
	d.isLocal = false

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, ip, err := d.Lookup(context.Background(), tt.hostname)
			assert.Error(t, err, ErrInvalidIP)
			assert.False(t, ok)
			assert.Nil(t, ip)
		})
	}
}

func TestLookupMulti_SingleIP(t *testing.T) {
	d := NewResolver()
	ok, ips, err := d.LookupMulti(context.Background(), "app.agentuity.com")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, ips)
	assert.GreaterOrEqual(t, len(ips), 1, "should return at least one IP")
}

func TestLookupMulti_Localhost(t *testing.T) {
	d := NewResolver()
	ok, ips, err := d.LookupMulti(context.Background(), "localhost")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Len(t, ips, 1)
	assert.Equal(t, "127.0.0.1", ips[0].String())
}

func TestLookupMulti_IPv4Address(t *testing.T) {
	d := NewResolver()
	ok, ips, err := d.LookupMulti(context.Background(), "8.8.8.8")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Len(t, ips, 1)
	assert.Equal(t, "8.8.8.8", ips[0].String())
}

func TestLookupMulti_GoogleMetadata(t *testing.T) {
	d := NewResolver()
	ok, ips, err := d.LookupMulti(context.Background(), googleMetadataHostname)
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Len(t, ips, 1)
	assert.Equal(t, magicIpAddress, ips[0].String())
}

func TestLookupMulti_InvalidDomain(t *testing.T) {
	d := NewResolver()
	ok, ips, err := d.LookupMulti(context.Background(), "nonexistent.invalid.domain.test")
	assert.Error(t, err)
	assert.False(t, ok)
	assert.Nil(t, ips)
}

func TestLookupMulti_Cached(t *testing.T) {
	c := cache.NewInMemory(context.Background(), cache.WithExpires(time.Hour))
	defer c.Close()
	d := NewResolver(WithCache(c))

	ok, ips, err := d.LookupMulti(context.Background(), "app.agentuity.com")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.GreaterOrEqual(t, len(ips), 1)

	ok, ips2, err := d.LookupMulti(context.Background(), "app.agentuity.com")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, ips, ips2, "cached result should match")

	ok, count := c.Hits("dns:app.agentuity.com")
	assert.True(t, ok)
	assert.Equal(t, 1, count, "should have one cache hit")
}

func TestLookupMulti_PrivateIPRejected(t *testing.T) {
	d := NewResolver(WithFailIfLocal())
	ok, ips, err := d.LookupMulti(context.Background(), "localhost")
	assert.Error(t, err)
	assert.False(t, ok)
	assert.Nil(t, ips)
}

func TestLookupMulti_ReturnsAllARecords(t *testing.T) {
	d := NewResolver()
	ok, ips, err := d.LookupMulti(context.Background(), "app.agentuity.com")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, ips)
	for _, ip := range ips {
		assert.NotNil(t, ip, "each IP should not be nil")
		assert.NotEmpty(t, ip.String(), "each IP should have a string representation")
	}
}

func TestLookupMulti_MixedPublicAndPrivateIPs(t *testing.T) {
	d := NewResolver(WithFailIfLocal())

	ok, ips, err := d.LookupMulti(context.Background(), "app.agentuity.com")
	require.NoError(t, err)
	require.True(t, ok)

	for _, ip := range ips {
		assert.False(t, ip.IsPrivate(), "should only return public IPs")
		assert.False(t, ip.IsLoopback(), "should not return loopback IPs")
	}
}

func TestLookupMulti_AllPrivateIPs(t *testing.T) {
	d := NewResolver(WithFailIfLocal())

	ok, ips, err := d.LookupMulti(context.Background(), "customer1.app.localhost.my.company.127.0.0.1.nip.io")
	assert.Error(t, err)
	assert.False(t, ok)
	assert.Nil(t, ips)
	assert.Equal(t, ErrInvalidIP, err)
}

func TestLookupMulti_AllowsPrivateIPsWhenLocal(t *testing.T) {
	c := cache.NewInMemory(context.Background(), cache.WithExpires(time.Hour))
	defer c.Close()
	d := NewResolver(WithCache(c))

	c.SetContext(context.Background(), "dns:test-private-local.example.com", []net.IP{
		net.ParseIP("10.0.0.1"),
		net.ParseIP("192.168.1.1"),
	}, time.Hour)

	ok, ips, err := d.LookupMulti(context.Background(), "test-private-local.example.com")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Len(t, ips, 2, "should return all IPs when isLocal=true")
}

func TestLookupMulti_CacheReturnsSliceDirectly(t *testing.T) {
	c := cache.NewInMemory(context.Background(), cache.WithExpires(time.Hour))
	defer c.Close()
	d := NewResolver(WithCache(c))

	originalIPs := []net.IP{
		net.ParseIP("8.8.8.8"),
		net.ParseIP("1.1.1.1"),
	}
	c.SetContext(context.Background(), "dns:test-cache.example.com", originalIPs, time.Hour)

	ok, ips, err := d.LookupMulti(context.Background(), "test-cache.example.com")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, originalIPs, ips, "should return the cached slice directly")
}

func TestLookupMulti_ContextCancellation(t *testing.T) {
	d := NewResolver()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ok, ips, err := d.LookupMulti(ctx, "app.agentuity.com")
	assert.Error(t, err)
	assert.False(t, ok)
	assert.Nil(t, ips)
}

func TestLookupMulti_LookupReturnsAllIPsThenRandom(t *testing.T) {
	d := NewResolver()

	ok, ips, err := d.LookupMulti(context.Background(), "app.agentuity.com")
	if assert.NoError(t, err) && assert.True(t, ok) && assert.GreaterOrEqual(t, len(ips), 1) {
		ok2, singleIP, err := d.Lookup(context.Background(), "app.agentuity.com")
		assert.NoError(t, err)
		assert.True(t, ok2)
		assert.NotNil(t, singleIP)

		found := false
		for _, ip := range ips {
			if ip.Equal(*singleIP) {
				found = true
				break
			}
		}
		assert.True(t, found, "Lookup should return one of the IPs from LookupMulti")
	}
}

func TestLookupMulti_IPv6Address(t *testing.T) {
	d := NewResolver()
	// IPv6 literals are not handled directly (only IPv4 regex is checked)
	// They go through DNS lookup which will fail for "::1"
	ok, ips, err := d.LookupMulti(context.Background(), "::1")
	// This is expected behavior: IPv6 literals require DNS resolution
	// which fails because "::1" is not a valid DNS name
	assert.False(t, ok)
	assert.Error(t, err)
	assert.Nil(t, ips)
}

// TestLookupMulti_CacheWithSerializedData tests that LookupMulti correctly
// deserializes []net.IP from []byte when using serialized caches (e.g., SQLite).
func TestLookupMulti_CacheWithSerializedData(t *testing.T) {
	// Use mockSerializedCache which simulates SQLite-backed cache that serializes to []byte
	mockCache := newMockSerializedCache()
	d := NewResolver(WithCache(mockCache))

	// Pre-populate cache with serialized []net.IP
	testIPs := []net.IP{
		net.ParseIP("8.8.8.8"),
		net.ParseIP("1.1.1.1"),
	}
	cacheKey := "dns:cached.example.com"
	err := mockCache.Set(cacheKey, testIPs, time.Hour)
	require.NoError(t, err)

	// LookupMulti should deserialize from []byte and return the cached IPs
	ok, ips, err := d.LookupMulti(context.Background(), "cached.example.com")
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, ips, 2)

	assert.Equal(t, "8.8.8.8", ips[0].String())
	assert.Equal(t, "1.1.1.1", ips[1].String())
}

// mockSerializedCache simulates a SQLite-backed cache that serializes values
type mockSerializedCache struct {
	data map[string][]byte
}

func newMockSerializedCache() *mockSerializedCache {
	return &mockSerializedCache{data: make(map[string][]byte)}
}

func (m *mockSerializedCache) Get(key string) (bool, any, error) {
	data, ok := m.data[key]
	if !ok {
		return false, nil, nil
	}
	return true, data, nil
}

func (m *mockSerializedCache) GetContext(ctx context.Context, key string) (bool, any, error) {
	return m.Get(key)
}

func (m *mockSerializedCache) Set(key string, val any, expires time.Duration) error {
	return m.SetContext(context.Background(), key, val, expires)
}

func (m *mockSerializedCache) SetContext(ctx context.Context, key string, val any, expires time.Duration) error {
	// Simulate serialization to []byte using msgpack
	data, err := msgpack.Marshal(val)
	if err != nil {
		return err
	}
	m.data[key] = data
	return nil
}

func (m *mockSerializedCache) Hits(key string) (bool, int) {
	return false, 0
}

func (m *mockSerializedCache) HitsContext(ctx context.Context, key string) (bool, int) {
	return m.Hits(key)
}

func (m *mockSerializedCache) Expire(key string) (bool, error) {
	delete(m.data, key)
	return true, nil
}

func (m *mockSerializedCache) ExpireContext(ctx context.Context, key string) (bool, error) {
	return m.Expire(key)
}

func (m *mockSerializedCache) Close() error {
	return nil
}

func (m *mockSerializedCache) CloseContext(ctx context.Context) error {
	return m.Close()
}

// TestLookupMulti_SerializedCache tests that LookupMulti works with
// caches that return serialized []byte instead of the original type.
func TestLookupMulti_SerializedCache(t *testing.T) {
	mockCache := newMockSerializedCache()

	// Pre-populate with serialized []net.IP
	testIPs := []net.IP{
		net.ParseIP("8.8.8.8"),
		net.ParseIP("1.1.1.1"),
	}
	cacheKey := "dns:cached.example.com"
	err := mockCache.Set(cacheKey, testIPs, time.Hour)
	require.NoError(t, err)

	// Create DNS resolver with the mock serialized cache
	d := &Dns{
		cache:   mockCache,
		isLocal: false,
	}

	// This should work even though the cache returns []byte, not []net.IP
	ok, ips, err := d.LookupMulti(context.Background(), "cached.example.com")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Len(t, ips, 2)
	assert.Equal(t, "8.8.8.8", ips[0].String())
	assert.Equal(t, "1.1.1.1", ips[1].String())
}

// TestLookup_SerializedCache tests the same issue for Lookup()
func TestLookup_SerializedCache(t *testing.T) {
	mockCache := newMockSerializedCache()

	testIPs := []net.IP{
		net.ParseIP("8.8.8.8"),
		net.ParseIP("1.1.1.1"),
	}
	cacheKey := "dns:cached.example.com"
	err := mockCache.Set(cacheKey, testIPs, time.Hour)
	require.NoError(t, err)

	d := &Dns{
		cache:   mockCache,
		isLocal: false,
	}

	ok, ip, err := d.Lookup(context.Background(), "cached.example.com")

	require.NoError(t, err)
	require.True(t, ok)
	assert.NotNil(t, ip)
	assert.Contains(t, []string{"8.8.8.8", "1.1.1.1"}, ip.String())
}
