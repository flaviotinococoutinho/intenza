package gtin

// Transcribed from https://www.gs1.org/standards/id-keys/company-prefix on
// 2026-10-14. "Prefixes not explicitly listed above are reserved by GS1 Global
// Office for future use."

// PrefixKind is what a GS1 Prefix range is used for.
type PrefixKind uint8

const (
	Reserved              PrefixKind = iota // reserved by GS1 Global Office
	MemberOrganization                      // issued by a national GS1 member organization
	RestrictedCirculation                   // internal or regional numbers, not globally unique
	GlobalOffice                            // GS1 Global Office allocations
	Demonstration                           // examples and demonstrations
	Bookland                                // ISBN (978, 979)
	SerialPublication                       // ISSN (977)
	Refund                                  // refund receipts
	Coupon                                  // coupons
)

// PrefixEntry is a handle to a row of the GS1 Prefix table.
type PrefixEntry uint8

type prefixRow struct {
	lo, hi uint16
	kind   PrefixKind
	org    string
}

var prefixRows = [...]prefixRow{
	{0, 999, Reserved, "Reserved by GS1 Global Office"},
	{0, 19, MemberOrganization, "GS1 US"},
	{20, 29, RestrictedCirculation, "Restricted circulation within a geographic region"},
	{30, 39, MemberOrganization, "GS1 US"},
	{40, 49, RestrictedCirculation, "Restricted circulation within a company"},
	{50, 59, MemberOrganization, "GS1 US (reserved for future use)"},
	{60, 139, MemberOrganization, "GS1 US"},
	{200, 299, RestrictedCirculation, "Restricted circulation within a geographic region"},
	{300, 379, MemberOrganization, "GS1 France"},
	{380, 380, MemberOrganization, "GS1 Bulgaria"},
	{381, 381, MemberOrganization, "GS1 Kosovo"},
	{383, 383, MemberOrganization, "GS1 Slovenija"},
	{385, 385, MemberOrganization, "GS1 Croatia"},
	{387, 387, MemberOrganization, "GS1 BIH (Bosnia-Herzegovina)"},
	{389, 389, MemberOrganization, "GS1 Montenegro"},
	{400, 440, MemberOrganization, "GS1 Germany"},
	{450, 459, MemberOrganization, "GS1 Japan"},
	{460, 469, MemberOrganization, "GS1 Russia"},
	{470, 470, MemberOrganization, "GS1 Kyrgyzstan"},
	{471, 471, MemberOrganization, "GS1 Chinese Taipei"},
	{474, 474, MemberOrganization, "GS1 Estonia"},
	{475, 475, MemberOrganization, "GS1 Latvia"},
	{476, 476, MemberOrganization, "GS1 Azerbaijan"},
	{477, 477, MemberOrganization, "GS1 Lithuania"},
	{478, 478, MemberOrganization, "GS1 Uzbekistan"},
	{479, 479, MemberOrganization, "GS1 Sri Lanka"},
	{480, 480, MemberOrganization, "GS1 Philippines"},
	{481, 481, MemberOrganization, "GS1 Belarus"},
	{482, 482, MemberOrganization, "GS1 Ukraine"},
	{483, 483, MemberOrganization, "GS1 Turkmenistan"},
	{484, 484, MemberOrganization, "GS1 Moldova"},
	{485, 485, MemberOrganization, "GS1 Armenia"},
	{486, 486, MemberOrganization, "GS1 Georgia"},
	{487, 487, MemberOrganization, "GS1 Kazakstan"},
	{488, 488, MemberOrganization, "GS1 Tajikistan"},
	{489, 489, MemberOrganization, "GS1 Hong Kong, China"},
	{490, 499, MemberOrganization, "GS1 Japan"},
	{500, 509, MemberOrganization, "GS1 UK"},
	{520, 521, MemberOrganization, "GS1 Association Greece"},
	{528, 528, MemberOrganization, "GS1 Lebanon"},
	{529, 529, MemberOrganization, "GS1 Cyprus"},
	{530, 530, MemberOrganization, "GS1 Albania"},
	{531, 531, MemberOrganization, "GS1 Macedonia"},
	{535, 535, MemberOrganization, "GS1 Malta"},
	{539, 539, MemberOrganization, "GS1 Ireland"},
	{540, 549, MemberOrganization, "GS1 Belgium & Luxembourg"},
	{560, 560, MemberOrganization, "GS1 Portugal"},
	{569, 569, MemberOrganization, "GS1 Iceland"},
	{570, 579, MemberOrganization, "GS1 Denmark"},
	{590, 590, MemberOrganization, "GS1 Poland"},
	{594, 594, MemberOrganization, "GS1 Romania"},
	{599, 599, MemberOrganization, "GS1 Hungary"},
	{600, 601, MemberOrganization, "GS1 South Africa"},
	{603, 603, MemberOrganization, "GS1 Ghana"},
	{604, 604, MemberOrganization, "GS1 Senegal"},
	{605, 605, MemberOrganization, "GS1 Uganda"},
	{606, 606, MemberOrganization, "GS1 Angola"},
	{607, 607, MemberOrganization, "GS1 Oman"},
	{608, 608, MemberOrganization, "GS1 Bahrain"},
	{609, 609, MemberOrganization, "GS1 Mauritius"},
	{610, 610, GlobalOffice, "Managed by GS1 Global Office for future MO"},
	{611, 611, MemberOrganization, "GS1 Morocco"},
	{613, 613, MemberOrganization, "GS1 Algeria"},
	{614, 614, GlobalOffice, "Managed by GS1 Global Office for future MO"},
	{615, 615, MemberOrganization, "GS1 Nigeria"},
	{616, 616, MemberOrganization, "GS1 Kenya"},
	{617, 617, MemberOrganization, "GS1 Cameroon"},
	{618, 618, MemberOrganization, "GS1 Côte d'Ivoire"},
	{619, 619, MemberOrganization, "GS1 Tunisia"},
	{620, 620, MemberOrganization, "GS1 Tanzania"},
	{621, 621, MemberOrganization, "GS1 Syria"},
	{622, 622, MemberOrganization, "GS1 Egypt"},
	{623, 623, GlobalOffice, "Managed by GS1 Global Office for future MO"},
	{624, 624, MemberOrganization, "GS1 Libya"},
	{625, 625, MemberOrganization, "GS1 Jordan"},
	{626, 626, MemberOrganization, "GS1 Iran"},
	{627, 627, MemberOrganization, "GS1 Kuwait"},
	{628, 628, MemberOrganization, "GS1 Saudi Arabia"},
	{629, 629, MemberOrganization, "GS1 Emirates"},
	{630, 630, MemberOrganization, "GS1 Qatar"},
	{631, 631, MemberOrganization, "GS1 Namibia"},
	{632, 632, MemberOrganization, "GS1 Rwanda"},
	{640, 649, MemberOrganization, "GS1 Finland"},
	{680, 681, MemberOrganization, "GS1 China"},
	{690, 699, MemberOrganization, "GS1 China"},
	{700, 709, MemberOrganization, "GS1 Norway"},
	{729, 729, MemberOrganization, "GS1 Israel"},
	{730, 739, MemberOrganization, "GS1 Sweden"},
	{740, 740, MemberOrganization, "GS1 Guatemala"},
	{741, 741, MemberOrganization, "GS1 El Salvador"},
	{742, 742, MemberOrganization, "GS1 Honduras"},
	{743, 743, MemberOrganization, "GS1 Nicaragua"},
	{744, 744, MemberOrganization, "GS1 Costa Rica"},
	{745, 745, MemberOrganization, "GS1 Panama"},
	{746, 746, MemberOrganization, "GS1 Republica Dominicana"},
	{750, 750, MemberOrganization, "GS1 Mexico"},
	{754, 755, MemberOrganization, "GS1 Canada"},
	{758, 758, GlobalOffice, "Managed by GS1 Global Office for future MO"},
	{759, 759, MemberOrganization, "GS1 Venezuela"},
	{760, 769, MemberOrganization, "GS1 Switzerland"},
	{770, 771, MemberOrganization, "GS1 Colombia"},
	{773, 773, MemberOrganization, "GS1 Uruguay"},
	{775, 775, MemberOrganization, "GS1 Peru"},
	{777, 777, MemberOrganization, "GS1 Bolivia"},
	{778, 779, MemberOrganization, "GS1 Argentina"},
	{780, 780, MemberOrganization, "GS1 Chile"},
	{784, 784, MemberOrganization, "GS1 Paraguay"},
	{786, 786, MemberOrganization, "GS1 Ecuador"},
	{789, 790, MemberOrganization, "GS1 Brasil"},
	{800, 839, MemberOrganization, "GS1 Italy"},
	{840, 849, MemberOrganization, "GS1 Spain"},
	{850, 850, MemberOrganization, "GS1 Cuba"},
	{858, 858, MemberOrganization, "GS1 Slovakia"},
	{859, 859, MemberOrganization, "GS1 Czech"},
	{860, 860, MemberOrganization, "GS1 Serbia"},
	{865, 865, MemberOrganization, "GS1 Mongolia"},
	{867, 867, MemberOrganization, "GS1 North Korea"},
	{868, 869, MemberOrganization, "GS1 Türkiye"},
	{870, 879, MemberOrganization, "GS1 Netherlands"},
	{880, 881, MemberOrganization, "GS1 South Korea"},
	{883, 883, MemberOrganization, "GS1 Myanmar"},
	{884, 884, MemberOrganization, "GS1 Cambodia"},
	{885, 885, MemberOrganization, "GS1 Thailand"},
	{887, 887, MemberOrganization, "GS1 Laos"},
	{888, 888, MemberOrganization, "GS1 Singapore"},
	{890, 890, MemberOrganization, "GS1 India"},
	{893, 893, MemberOrganization, "GS1 Vietnam"},
	{894, 894, GlobalOffice, "Managed by GS1 Global Office for future MO"},
	{896, 896, MemberOrganization, "GS1 Pakistan"},
	{899, 899, MemberOrganization, "GS1 Indonesia"},
	{900, 919, MemberOrganization, "GS1 Austria"},
	{930, 939, MemberOrganization, "GS1 Australia"},
	{940, 949, MemberOrganization, "GS1 New Zealand"},
	{950, 950, GlobalOffice, "GS1 Global Office"},
	{951, 951, GlobalOffice, "Global Office - General Manager Number"},
	{952, 952, Demonstration, "Demonstrations and examples of the GS1 system"},
	{955, 955, MemberOrganization, "GS1 Malaysia"},
	{958, 958, MemberOrganization, "GS1 Macau, China"},
	{960, 969, GlobalOffice, "Global Office - GTIN-8"},
	{977, 977, SerialPublication, "Serial publications (ISSN)"},
	{978, 979, Bookland, "Bookland (ISBN)"},
	{980, 980, Refund, "Refund receipts"},
	{981, 983, Coupon, "Coupon identification for common currency areas"},
	{990, 999, Coupon, "Coupon identification"},
}

// prefixTable maps every three-digit prefix to its row: later rows override
// the reserved default, so gaps stay reserved. One byte per prefix.
var prefixTable = func() (t [1000]PrefixEntry) {
	for i := 1; i < len(prefixRows); i++ {
		for p := prefixRows[i].lo; p <= prefixRows[i].hi; p++ {
			t[p] = PrefixEntry(i)
		}
	}
	return t
}()

// Kind is what the range is used for.
func (e PrefixEntry) Kind() PrefixKind { return prefixRows[e].kind }

// Org is the issuing organization or the use of the range, as GS1 names it.
func (e PrefixEntry) Org() string { return prefixRows[e].org }

// Range is the inclusive interval of three-digit prefixes of the row.
func (e PrefixEntry) Range() (lo, hi int) { return int(prefixRows[e].lo), int(prefixRows[e].hi) }
