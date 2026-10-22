package outbound

import "testing"

func TestLegacyJSONEndpointIsExact(t *testing.T) {
	if _, err := NetcupLegacyDNS.URL("https://ccp.netcup.net/run/webservice/servers/endpoint.php?JSON"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"https://ccp.netcup.net/run/webservice/servers/endpoint.php?WSDL", "https://ccp.netcup.net/run/webservice/servers/endpoint.php/escape?JSON", "https://ccp.netcup.net/run/webservice/servers/endpoint.php?JSON&target=other", "https://api.netcup.com/v1/domain", "http://ccp.netcup.net/run/webservice/servers/endpoint.php?JSON", "https://user:secret@ccp.netcup.net/run/webservice/servers/endpoint.php?JSON", "https://ccp.netcup.net/run/webservice/servers/../endpoint.php?JSON"} {
		if _, err := NetcupLegacyDNS.URL(raw); err == nil {
			t.Fatal("endpoint escape")
		}
	}
}
